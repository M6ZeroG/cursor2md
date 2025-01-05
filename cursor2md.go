package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// 定义JSON结构体
type ChatRecord struct {
	Conversation []Message `json:"conversation"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Context      struct {
		FileSelections []struct {
			Uri struct {
				Path string `json:"path"`
			} `json:"uri"`
		} `json:"fileSelections"`
	} `json:"context"`
	CreatedAt int64 `json:"createdAt"`
	EndedAt   int64
}

type Message struct {
	Type    int    `json:"type"`
	Text    string `json:"text"`
	Context struct {
		FileSelections []struct {
			Uri struct {
				Path string `json:"path"`
			} `json:"uri"`
		} `json:"fileSelections"`
		Selections []struct {
			Text string `json:"text"`
			Uri  struct {
				Path string `json:"path"`
			} `json:"uri"`
		} `json:"selections"`
	} `json:"context"`
	TimingInfo struct {
		ClientStartTime int64 `json:"clientStartTime"`
		ClientEndTime   int64 `json:"clientEndTime"`
	} `json:"timingInfo"`
	CodeBlocks []struct {
		Uri struct {
			Path string `json:"path"`
		} `json:"uri"`
		Content    string `json:"content"`
		LanguageId string `json:"languageId"`
	} `json:"codeBlocks"`
}

// 获取state.vscdb的默认路径
func getDefaultDBPath() string {
	var basePath string
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("获取用户主目录失败: %v\n", err)
		return ""
	}

	switch runtime.GOOS {
	case "windows":
		// Windows: %APPDATA%/Cursor/User/globalStorage
		basePath = filepath.Join(os.Getenv("APPDATA"), "Cursor", "User", "globalStorage")
	case "darwin":
		// macOS: ~/Library/Application Support/Cursor/User/globalStorage
		basePath = filepath.Join(homeDir, "Library", "Application Support", "Cursor", "User", "globalStorage")
	case "linux":
		// Linux: ~/.config/Cursor/User/workspaceStorage
		basePath = filepath.Join(homeDir, ".config", "Cursor", "User", "workspaceStorage")
	default:
		fmt.Printf("不支持的操作系统: %s\n", runtime.GOOS)
		return ""
	}

	return filepath.Join(basePath, "state.vscdb")
}

// 在main函数前添加新的结构体用于存储命令行参数
type Config struct {
	DBPath        string    // 数据库路径
	OutputDir     string    // 输出目录路径
	StartAfter    time.Time // 开始时间下限
	StartBefore   time.Time // 开始时间上限
	EndAfter      time.Time // 结束时间下限
	EndBefore     time.Time // 结束时间上限
	HasTimeFilter bool      // 是否启用时间过滤
}

// 修改时间过滤函数
func (c *Config) isInTimeRange(record ChatRecord) bool {
	if !c.HasTimeFilter {
		return true
	}

	// 转换记录中的时间戳
	startTime := time.Unix(record.CreatedAt/1000, 0)

	// 获取结束时间
	if len(record.Conversation) > 0 {
		record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
	}

	var endTime time.Time
	if record.EndedAt > 0 {
		endTime = time.Unix(record.EndedAt/1000, 0)
	}

	// 检查开始时间范围
	if !c.StartAfter.IsZero() && startTime.Before(c.StartAfter) {
		fmt.Printf("跳过: %s 的开始时间 %s 早于筛选时间 %s\n",
			record.Name, startTime.Format("2006-01-02 15:04:05"),
			c.StartAfter.Format("2006-01-02 15:04:05"))
		return false
	}
	if !c.StartBefore.IsZero() && startTime.After(c.StartBefore) {
		fmt.Printf("跳过: %s 的开始时间 %s 晚于筛选时间 %s\n",
			record.Name, startTime.Format("2006-01-02 15:04:05"),
			c.StartBefore.Format("2006-01-02 15:04:05"))
		return false
	}

	// 检查结束时间范围（只在有效的结束时间时进行检查）
	if record.EndedAt > 0 {
		if !c.EndAfter.IsZero() && endTime.Before(c.EndAfter) {
			fmt.Printf("跳过: %s 的结束时间 %s 早于筛选时间 %s\n",
				record.Name, endTime.Format("2006-01-02 15:04:05"),
				c.EndAfter.Format("2006-01-02 15:04:05"))
			return false
		}
		if !c.EndBefore.IsZero() && endTime.After(c.EndBefore) {
			fmt.Printf("跳过: %s 的结束时间 %s 晚于筛选时间 %s\n",
				record.Name, endTime.Format("2006-01-02 15:04:05"),
				c.EndBefore.Format("2006-01-02 15:04:05"))
			return false
		}
	}

	// 添加调试信息
	fmt.Printf("保留: %s (开始时间: %s, 结束时间: %s)\n",
		record.Name,
		startTime.Format("2006-01-02 15:04:05"),
		endTime.Format("2006-01-02 15:04:05"))
	return true
}

// 添加时间解析函数
func parseTimeArg(timeStr string) (time.Time, error) {
	if timeStr == "" {
		return time.Time{}, nil
	}
	// 支持多种时间格式
	formats := []string{
		"2006-01-02",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.ParseInLocation(format, timeStr, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无效的时间格式: %s", timeStr)
}

// 处理单个数据库文件
func processDatabase(dbPath string, config Config) error {
	// 检查文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("数据库文件不存在: %s", dbPath)
	}

	// 打开SQLite数据库
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 创建输出目录
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	// 查询cursorDiskKV表中的所有记录
	rows, err := db.Query("SELECT key, value FROM cursorDiskKV")
	if err != nil {
		return fmt.Errorf("查询数据库失败: %v", err)
	}
	defer rows.Close()

	// 遍历每条记录
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			fmt.Printf("读取记录失败: %v\n\n", err)
			continue
		}

		// 跳过特殊情况
		if value == "[]" || key == "inlineDiffsData" {
			fmt.Printf("跳过特殊记录: key=%s, value=%s\n\n", key, value)
			continue
		}

		// 添加调试信息，打印原始JSON数据的前200个字符
		fmt.Printf("正在处理记录 key=%s\n", key)

		// 解析JSON
		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			fmt.Printf("解析JSON失败 (key=%s): %v\n", key, err)
			// 添加更多错误详情
			fmt.Printf("JSON长度: %d\n", len(value))
			// 检查JSON是否为空
			if len(value) == 0 {
				fmt.Println("警告: JSON数据为空")
				continue
			}
			// 检查JSON是否为有效的JSON格式
			if !json.Valid([]byte(value)) {
				fmt.Println("警告: 无效的JSON格式")
				// 尝试输出导致错误的位置附近的内容
				errorContext := 50 // 显示错误位置前后50个字符
				startPos := 0
				if len(value) > errorContext {
					startPos = len(value) - errorContext
				}
				fmt.Printf("JSON结尾部分: %s\n\n", value[startPos:])
			}
			continue
		}

		// 如果name为空，使用key作为文件名
		if record.Name == "" {
			record.Name = key
		}

		// 检查是否有实际内容
		if !hasValidContent(record) {
			fmt.Printf("跳过空记录: %s\n\n", record.Name)
			continue
		}

		// 添加时间过滤
		if !config.isInTimeRange(record) {
			fmt.Printf("跳过不在时间范围内的记录: %s\n", record.Name)
			continue
		}

		// 生成markdown内容
		mdContent := convertToMarkdown(record)

		// 创建markdown文件
		mdFile := filepath.Join(config.OutputDir, record.Name+".md")
		if err := ioutil.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
			fmt.Printf("写入markdown文件 %s 失败: %v\n\n", mdFile, err)
			continue
		}

		fmt.Printf("成功生成markdown文件: %s\n\n", mdFile)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历记录时发生错误: %v", err)
	}

	return nil
}

// 添加新的结构体用于存储会话信息
type SessionInfo struct {
	Hash      string    // 会话哈希值
	Title     string    // 会话标题
	StartTime time.Time // 开始时间
	EndTime   time.Time // 结束时间
}

// 添加新的函数用于列出会话信息
func listSessions(dbPath string) error {
	// 检查文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("数据库文件不存在: %s", dbPath)
	}

	// 打开SQLite数据库
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 查询所有记录
	rows, err := db.Query("SELECT key, value FROM cursorDiskKV")
	if err != nil {
		return fmt.Errorf("查询数据库失败: %v", err)
	}
	defer rows.Close()

	// 存储所有有效的会话信息
	var sessions []SessionInfo

	// 遍历记录
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		// 跳过特殊记录
		if value == "[]" || key == "inlineDiffsData" {
			continue
		}

		// 解析JSON
		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}

		// 跳过无效内容
		if !hasValidContent(record) {
			continue
		}

		// 获取结束时间
		if len(record.Conversation) > 0 {
			record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
		}

		// 创建会话信息
		session := SessionInfo{
			Hash:      strings.TrimPrefix(key, "composerData:"),
			Title:     record.Name,
			StartTime: time.Unix(record.CreatedAt/1000, 0),
			EndTime:   time.Unix(record.EndedAt/1000, 0),
		}

		sessions = append(sessions, session)
	}

	// 如果没有会话
	if len(sessions) == 0 {
		fmt.Println("数据库中没有有效的会话记录")
		return nil
	}

	// 计算最长的标题长度，用于对齐
	maxTitleLen := 0
	maxHashLen := 0
	for _, s := range sessions {
		if len(s.Title) > maxTitleLen {
			maxTitleLen = len(s.Title)
		}
		if len(s.Hash) > maxHashLen {
			maxHashLen = len(s.Hash)
		}
	}

	// 打印表头
	format := fmt.Sprintf("%%-%ds  %%10s  %%10s  %%-%ds\n", maxHashLen, maxTitleLen)
	fmt.Printf(format, "HASH", "START TIME", "END TIME", "TITLE")
	fmt.Printf(strings.Repeat("-", maxHashLen+maxTitleLen+24) + "\n")

	// 按开始时间排序
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})

	// 打印会话信息
	for _, s := range sessions {
		endTimeStr := s.EndTime.Format("2006-01-02")
		if s.EndTime.IsZero() {
			endTimeStr = "未结束"
		}
		fmt.Printf(format,
			s.Hash,
			s.StartTime.Format("2006-01-02"),
			endTimeStr,
			s.Title,
		)
	}

	// 打印统计信息
	fmt.Printf("\n共有 %d 个会话\n", len(sessions))
	return nil
}

func main() {
	var config Config
	var startAfterStr, startBeforeStr, endAfterStr, endBeforeStr string

	// 创建一个新的FlagSet用于ls命令
	lsCmd := flag.NewFlagSet("ls", flag.ExitOnError)
	lsDBPath := lsCmd.String("db", "", "数据库文件路径 (默认: 系统默认路径)")

	// 检查是否是ls命令
	if len(os.Args) > 1 && os.Args[1] == "ls" {
		lsCmd.Parse(os.Args[2:])
		dbPath := *lsDBPath
		if dbPath == "" {
			dbPath = getDefaultDBPath()
			if dbPath == "" {
				fmt.Println("无法确定默认数据库路径")
				return
			}
		}
		if err := listSessions(dbPath); err != nil {
			fmt.Printf("列出会话失败: %v\n", err)
		}
		return
	}

	// 定义命令行参数
	flag.StringVar(&config.DBPath, "db", "", "数据库文件路径 (默认: 系统默认路径)")
	flag.StringVar(&config.OutputDir, "out", "markdown_output", "markdown文件输出目录")
	flag.StringVar(&startAfterStr, "start-after", "", "仅包含在此时间之后开始的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
	flag.StringVar(&startBeforeStr, "start-before", "", "仅包含在此时间之前开始的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
	flag.StringVar(&endAfterStr, "end-after", "", "仅包含在此时间之后结束的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
	flag.StringVar(&endBeforeStr, "end-before", "", "仅包含在此时间之前结束的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
	flag.Parse()

	// 解析时间参数
	var err error
	if config.StartAfter, err = parseTimeArg(startAfterStr); err != nil {
		fmt.Printf("解析start-after参数失败: %v\n", err)
		return
	}
	if config.StartBefore, err = parseTimeArg(startBeforeStr); err != nil {
		fmt.Printf("解析start-before参数失败: %v\n", err)
		return
	}
	if config.EndAfter, err = parseTimeArg(endAfterStr); err != nil {
		fmt.Printf("解析end-after参数失败: %v\n", err)
		return
	}
	if config.EndBefore, err = parseTimeArg(endBeforeStr); err != nil {
		fmt.Printf("解析end-before参数失败: %v\n", err)
		return
	}

	// 检查是否启用了时间过滤
	config.HasTimeFilter = !config.StartAfter.IsZero() || !config.StartBefore.IsZero() ||
		!config.EndAfter.IsZero() || !config.EndBefore.IsZero()

	// 如果没有指定数据库路径，使用默认路径
	if config.DBPath == "" {
		config.DBPath = getDefaultDBPath()
		if config.DBPath == "" {
			fmt.Println("无法确定默认数据库路径")
			return
		}
	}

	// 处理数据库文件
	if err := processDatabase(config.DBPath, config); err != nil {
		fmt.Printf("处理数据库失败: %v\n", err)
		return
	}

	fmt.Println("处理完成!")
}

func convertToMarkdown(record ChatRecord) string {
	var md strings.Builder

	// 添加标题
	md.WriteString(fmt.Sprintf("# %s\n\n", record.Name))

	// 获取结束时间（最后一条消息的clientEndTime）
	if len(record.Conversation) > 0 {
		record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
	}

	// 添加会话信息
	md.WriteString("## 会话信息\n\n")
	md.WriteString(fmt.Sprintf("- 开始时间: \t%s\n", time.Unix(record.CreatedAt/1000, 0).Format("2006-01-02 15:04:05")))
	if record.EndedAt > 0 {
		md.WriteString(fmt.Sprintf("- 结束时间:\t%s\n", time.Unix(record.EndedAt/1000, 0).Format("2006-01-02 15:04:05")))
	}

	// 添加相关文件信息
	if len(record.Context.FileSelections) > 0 {
		md.WriteString("- 相关文件:\t")
		files := make([]string, 0, len(record.Context.FileSelections))
		for _, file := range record.Context.FileSelections {
			filename := filepath.Base(file.Uri.Path)
			files = append(files, fmt.Sprintf("[%s](%s)", filename, file.Uri.Path))
		}
		md.WriteString(strings.Join(files, "\t"))
		md.WriteString("\n")
	}
	md.WriteString("\n")

	// 添加对话内容
	for _, msg := range record.Conversation {
		switch msg.Type {
		case 1: // 用户消息
			md.WriteString("## User\n\n")

			// 添加引用的文件
			if len(msg.Context.FileSelections) > 0 {
				md.WriteString("引用的文件:\t")
				files := make([]string, 0, len(msg.Context.FileSelections))
				for _, file := range msg.Context.FileSelections {
					filename := filepath.Base(file.Uri.Path)
					files = append(files, fmt.Sprintf("[%s](%s)", filename, file.Uri.Path))
				}
				md.WriteString(strings.Join(files, "\t"))
				md.WriteString("\n\n")
			}

			// 添加引用的代码片段
			if len(msg.Context.Selections) > 0 {
				md.WriteString("引用的代码片段:\n")
				for _, sel := range msg.Context.Selections {
					if sel.Uri.Path != "" {
						filename := filepath.Base(sel.Uri.Path)
						md.WriteString(fmt.Sprintf("From [%s](%s):\n", filename, sel.Uri.Path))
					}
					md.WriteString(sel.Text)
					md.WriteString("\n")
				}
			}

			// 添加消息文本
			md.WriteString("> " + msg.Text + "\n\n")

		case 2: // AI回复
			md.WriteString("## Cursor\n\n")
			md.WriteString(msg.Text + "\n\n")

			// 处理代码块
			for _, block := range msg.CodeBlocks {
				if block.Content != "" {
					if block.Uri.Path != "" {
						filename := filepath.Base(block.Uri.Path)
						md.WriteString(fmt.Sprintf("```%s:[%s](%s)\n", block.LanguageId, filename, block.Uri.Path))
					} else {
						md.WriteString(fmt.Sprintf("```%s\n", block.LanguageId))
					}
					md.WriteString(block.Content + "\n")
					md.WriteString("```\n\n")
				}
			}
		}
	}

	return md.String()
}

// 检查记录是否包含有效内容
func hasValidContent(record ChatRecord) bool {
	// 检查名称是否为composerData开头
	if strings.HasPrefix(record.Name, "composerData:") {
		return false
	}

	// 检查是否有对话内容
	if len(record.Conversation) == 0 {
		return false
	}

	// 检查对话是否有实际文本
	hasText := false
	for _, msg := range record.Conversation {
		if msg.Text != "" {
			hasText = true
			break
		}
	}

	return hasText
}
