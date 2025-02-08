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

// 定义命令行参数配置
type Config struct {
	DBPath        string    // 数据库路径
	OutputDir     string    // 输出目录路径
	StartAfter    time.Time // 开始时间下限
	StartBefore   time.Time // 开始时间上限
	EndAfter      time.Time // 结束时间下限
	EndBefore     time.Time // 结束时间上限
	HasTimeFilter bool      // 是否启用时间过滤
	JsonOutput    bool      // 是否输出JSON格式
	SortDesc      bool      // 是否按时间降序排序（从新到旧）
	ByName        bool      // 是否在文件名前添加序号
}

// 检查记录是否包含有效内容
func hasValidContent(record ChatRecord) bool {
	if strings.HasPrefix(record.Name, "composerData:") {
		return false
	}
	if len(record.Conversation) == 0 {
		return false
	}
	return true
}

// 解析时间参数
func parseTimeArg(timeStr string) (time.Time, error) {
	if timeStr == "" {
		return time.Time{}, nil
	}
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

// 会话信息结构体
type SessionInfo struct {
	Hash      string    // 会话哈希值
	Title     string    // 会话标题
	StartTime time.Time // 开始时间
	EndTime   time.Time // 结束时间
}

// 在SessionInfo结构体后添加新的结构体
type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
	Total    int           `json:"total"`
	Success  bool          `json:"success"`
	Error    *string       `json:"error,omitempty"`
}

// 在文件开头添加新的结构体定义
type VersionResponse struct {
	Version string `json:"version"`
	Success bool   `json:"success"`
}

type ExportedSession struct {
	Hash       string    `json:"hash"`
	Title      string    `json:"title"`
	OutputPath string    `json:"outputPath"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
}

type ExportResponse struct {
	Success  bool              `json:"success"`
	Exported []ExportedSession `json:"exported"`
	Total    int               `json:"total"`
	Error    *string           `json:"error,omitempty"`
}

// 修改listSessions函数，添加json参数
func listSessions(dbPath string, jsonOutput bool) error {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if jsonOutput {
			errMsg := fmt.Sprintf("数据库文件不存在: %s", dbPath)
			response := SessionListResponse{
				Sessions: nil,
				Total:    0,
				Success:  false,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("数据库文件不存在: %s", dbPath)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		if jsonOutput {
			errMsg := fmt.Sprintf("打开数据库失败: %v", err)
			response := SessionListResponse{
				Sessions: nil,
				Total:    0,
				Success:  false,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT key, value FROM cursorDiskKV")
	if err != nil {
		return fmt.Errorf("查询数据库失败: %v", err)
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		if value == "[]" || key == "inlineDiffsData" {
			continue
		}

		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}
		if !hasValidContent(record) {
			continue
		}

		if len(record.Conversation) > 0 {
			record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
		}

		session := SessionInfo{
			Hash:      strings.TrimPrefix(key, "composerData:"),
			Title:     record.Name,
			StartTime: time.Unix(record.CreatedAt/1000, 0),
			EndTime:   time.Unix(record.EndedAt/1000, 0),
		}
		sessions = append(sessions, session)
	}

	if jsonOutput {
		response := SessionListResponse{
			Sessions: sessions,
			Total:    len(sessions),
			Success:  true,
		}
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON序列化失败: %v", err)
		}
		fmt.Println(string(jsonData))
		return nil
	}

	if len(sessions) == 0 {
		fmt.Println("数据库中没有有效的会话记录")
		return nil
	}

	maxTitleLen, maxHashLen := 0, 0
	for _, s := range sessions {
		if len(s.Title) > maxTitleLen {
			maxTitleLen = len(s.Title)
		}
		if len(s.Hash) > maxHashLen {
			maxHashLen = len(s.Hash)
		}
	}

	format := fmt.Sprintf("%%-%ds  %%10s  %%10s  %%-%ds\n", maxHashLen, maxTitleLen)
	fmt.Printf(format, "HASH", "START TIME", "END TIME", "TITLE")
	fmt.Printf(strings.Repeat("-", maxHashLen+maxTitleLen+24) + "\n")

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})

	for _, s := range sessions {
		endTimeStr := s.EndTime.Format("2006-01-02")
		if s.EndTime.IsZero() {
			endTimeStr = "未结束"
		}
		fmt.Printf(format, s.Hash, s.StartTime.Format("2006-01-02"), endTimeStr, s.Title)
	}

	fmt.Printf("\n共有 %d 个会话\n", len(sessions))
	return nil
}

// 修改 generateNumberedFileName 函数
func generateNumberedFileName(totalSessions int, index int, descending bool, name string) string {
	// 计算需要的序号位数 (例如: 100条记录需要3位数)
	digits := len(fmt.Sprintf("%d", totalSessions))
	
	// 根据排序方式和索引生成序号
	var number int
	if descending {
		// 降序：最新的记录使用小序号
		number = index + 1
	} else {
		// 升序：最早的记录使用小序号
		number = index + 1
	}
	
	// 格式化序号为固定位数的字符串 (例如: 001, 002, ...)
	numberStr := fmt.Sprintf("%0*d", digits, number)
	
	// 如果文件名为空，使用默认名称
	if strings.TrimSpace(name) == "" {
		name = "untitled"
	}

	// 替换Windows文件系统不支持的字符
	safeName := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		"\"", "_",
		"/", "_",
		"\\", "_",
		"|", "_",
		"?", "_",
		"*", "_",
	).Replace(name)
	
	result := fmt.Sprintf("%s-%s.md", numberStr, safeName)
	return result
}

// 修改exportSessions函数
func exportSessions(config Config) error {
	if _, err := os.Stat(config.DBPath); os.IsNotExist(err) {
		if config.JsonOutput {
			errMsg := fmt.Sprintf("数据库文件不存在: %s", config.DBPath)
			response := ExportResponse{
				Success:  false,
				Exported: nil,
				Total:    0,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("数据库文件不存在: %s", config.DBPath)
	}

	db, err := sql.Open("sqlite3", config.DBPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer db.Close()

	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	rows, err := db.Query("SELECT key, value FROM cursorDiskKV")
	if err != nil {
		return fmt.Errorf("查询数据库失败: %v", err)
	}
	defer rows.Close()

	var exportedSessions []ExportedSession
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		if value == "[]" || key == "inlineDiffsData" {
			continue
		}

		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}
		if !hasValidContent(record) {
			continue
		}

		if len(record.Conversation) > 0 {
			record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
		}

		if !config.isInTimeRange(record) {
			continue
		}

		exportedSession := ExportedSession{
			Hash:       strings.TrimPrefix(key, "composerData:"),
			Title:      record.Name,
			StartTime:  time.Unix(record.CreatedAt/1000, 0),
			EndTime:    time.Unix(record.EndedAt/1000, 0),
		}
		exportedSessions = append(exportedSessions, exportedSession)
	}

	// 先对会话进行排序
	sortExportedSessions(exportedSessions, config.SortDesc)
	

	// 然后生成文件
	totalSessions := len(exportedSessions)
	for i, session := range exportedSessions {
		// 重新查询记录以获取完整内容
		key := "composerData:" + session.Hash
		var value string
		if err := db.QueryRow("SELECT value FROM cursorDiskKV WHERE key = ?", key).Scan(&value); err != nil {
			continue
		}

		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}

		mdContent := convertToMarkdown(record)
		var mdFile string
		if config.ByName {
			// 使用当前索引生成序号
			fileName := generateNumberedFileName(totalSessions, i, config.SortDesc, record.Name)
			mdFile = filepath.Join(config.OutputDir, fileName)
		} else {
			// 替换Windows文件系统不支持的字符
			safeName := strings.NewReplacer(
				"<", "_",
				">", "_",
				":", "_",
				"\"", "_",
				"/", "_",
				"\\", "_",
				"|", "_",
				"?", "_",
				"*", "_",
			).Replace(record.Name)
			
			// 如果文件名为空，使用默认名称
			if strings.TrimSpace(safeName) == "" {
				safeName = "untitled"
			}
			
			// 检查文件是否已存在，如果存在则添加时间戳
			baseFile := filepath.Join(config.OutputDir, safeName+".md")
			mdFile = baseFile
			if _, err := os.Stat(baseFile); err == nil {
				// 文件已存在，添加时间戳
				timestamp := session.StartTime.Format("20060102-150405")
				mdFile = filepath.Join(config.OutputDir, safeName+"-"+timestamp+".md")
			}
		}
		
		if err := ioutil.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
			continue
		}
		
		// 更新输出路径
		exportedSessions[i].OutputPath = mdFile
	}

	if config.JsonOutput {
		response := ExportResponse{
			Success:  true,
			Exported: exportedSessions,
			Total:    len(exportedSessions),
		}
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON序列化失败: %v", err)
		}
		fmt.Println(string(jsonData))
	} else {
		// 按时间顺序打印导出信息
		for _, session := range exportedSessions {
			fileName := filepath.Base(session.OutputPath)
			fmt.Printf("导出会话: %s (开始时间: %s)\n", 
				fileName,
				session.StartTime.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("\n成功导出 %d 个会话到 %s\n", len(exportedSessions), config.OutputDir)
	}

	return nil
}

// 检查时间范围
func (c *Config) isInTimeRange(record ChatRecord) bool {
	if !c.HasTimeFilter {
		return true
	}

	startTime := time.Unix(record.CreatedAt/1000, 0)
	if len(record.Conversation) > 0 {
		record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
	}
	var endTime time.Time
	if record.EndedAt > 0 {
		endTime = time.Unix(record.EndedAt/1000, 0)
	}

	if !c.StartAfter.IsZero() && startTime.Before(c.StartAfter) {
		return false
	}
	if !c.StartBefore.IsZero() && startTime.After(c.StartBefore) {
		return false
	}
	if record.EndedAt > 0 {
		if !c.EndAfter.IsZero() && endTime.Before(c.EndAfter) {
			return false
		}
		if !c.EndBefore.IsZero() && endTime.After(c.EndBefore) {
			return false
		}
	}

	return true
}

// 转换为Markdown
func convertToMarkdown(record ChatRecord) string {
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# %s\n\n", record.Name))

	if len(record.Conversation) > 0 {
		record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
	}

	md.WriteString("## 会话信息\n\n")
	md.WriteString(fmt.Sprintf("- 开始时间: \t%s\n", time.Unix(record.CreatedAt/1000, 0).Format("2006-01-02 15:04:05")))
	if record.EndedAt > 0 {
		md.WriteString(fmt.Sprintf("- 结束时间:\t%s\n", time.Unix(record.EndedAt/1000, 0).Format("2006-01-02 15:04:05")))
	}

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

	for _, msg := range record.Conversation {
		switch msg.Type {
		case 1:
			md.WriteString("## User\n\n")
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
			md.WriteString("> " + msg.Text + "\n\n")

		case 2:
			md.WriteString("## Cursor\n\n")
			md.WriteString(msg.Text + "\n\n")
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

// 修改exportSingleSession函数
func exportSingleSession(dbPath string, outputDir string, hash string, jsonOutput bool, sortDesc bool, byName bool) error {
	// 检查文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if jsonOutput {
			errMsg := fmt.Sprintf("数据库文件不存在: %s", dbPath)
			response := ExportResponse{
				Success:  false,
				Exported: nil,
				Total:    0,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("数据库文件不存在: %s", dbPath)
	}

	// 打开SQLite数据库
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	// 查询指定的会话记录
	key := "composerData:" + hash
	var value string
	err = db.QueryRow("SELECT value FROM cursorDiskKV WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		if jsonOutput {
			errMsg := fmt.Sprintf("未找到哈希值为 %s 的会话", hash)
			response := ExportResponse{
				Success:  false,
				Exported: nil,
				Total:    0,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("未找到哈希值为 %s 的会话", hash)
	}
	if err != nil {
		if jsonOutput {
			errMsg := fmt.Sprintf("查询数据库失败: %v", err)
			response := ExportResponse{
				Success:  false,
				Exported: nil,
				Total:    0,
				Error:    &errMsg,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
			return nil
		}
		return fmt.Errorf("查询数据库失败: %v", err)
	}

	// 解析JSON
	var record ChatRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	// 检查是否有效
	if !hasValidContent(record) {
		return fmt.Errorf("会话内容无效")
	}

	// 获取结束时间
	if len(record.Conversation) > 0 {
		record.EndedAt = record.Conversation[len(record.Conversation)-1].TimingInfo.ClientEndTime
	}

	// 生成markdown内容
	mdContent := convertToMarkdown(record)
	var mdFile string
	if byName {
		// 创建一个只包含当前会话的切片用于生成序号
		fileName := generateNumberedFileName(1, 0, sortDesc, record.Name)
		mdFile = filepath.Join(outputDir, fileName)
	} else {
		// 替换Windows文件系统不支持的字符
		safeName := strings.NewReplacer(
			"<", "_",
			">", "_",
			":", "_",
			"\"", "_",
			"/", "_",
			"\\", "_",
			"|", "_",
			"?", "_",
			"*", "_",
		).Replace(record.Name)
		
		// 如果文件名为空，使用默认名称
		if strings.TrimSpace(safeName) == "" {
			safeName = "untitled"
		}
		
		mdFile = filepath.Join(outputDir, safeName+".md")
	}

	if err := ioutil.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
		return fmt.Errorf("写入markdown文件失败: %v", err)
	}

	if jsonOutput {
		exportedSession := ExportedSession{
			Hash:       hash,
			Title:      record.Name,
			OutputPath: mdFile,
			StartTime:  time.Unix(record.CreatedAt/1000, 0),
			EndTime:    time.Unix(record.EndedAt/1000, 0),
		}
		response := ExportResponse{
			Success:  true,
			Exported: []ExportedSession{exportedSession},
			Total:    1,
		}
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON序列化失败: %v", err)
		}
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("成功导出会话: %s\n", record.Name)
	}

	return nil
}

// 添加排序函数
func sortExportedSessions(sessions []ExportedSession, descending bool) {
	sort.Slice(sessions, func(i, j int) bool {
		if descending {
			// 降序：新的在前（从新到旧）
			return sessions[i].StartTime.After(sessions[j].StartTime)
		}
		// 升序：旧的在前（从旧到新）
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	switch os.Args[1] {
	case "ls":
		lsCmd := flag.NewFlagSet("ls", flag.ExitOnError)
		lsDBPath := lsCmd.String("db", "", "数据库文件路径 (默认: 系统默认路径)")
		jsonOutput := lsCmd.Bool("json", false, "以JSON格式输出")
		lsCmd.Parse(os.Args[2:])
		dbPath := *lsDBPath
		if dbPath == "" {
			dbPath = getDefaultDBPath()
			if dbPath == "" {
				fmt.Println("无法确定默认数据库路径")
				return
			}
		}
		if err := listSessions(dbPath, *jsonOutput); err != nil {
			fmt.Printf("列出会话失败: %v\n", err)
		}

	case "export":
		// 检查是否提供了hash参数
		if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
			// 导出单个会话
			hash := os.Args[2]
			exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
			dbPath := exportCmd.String("db", "", "数据库文件路径 (默认: 系统默认路径)")
			outputDir := exportCmd.String("out", "markdown_output", "markdown文件输出目录")
			jsonOutput := exportCmd.Bool("json", false, "以JSON格式输出")
			sortDesc := exportCmd.Bool("sort-desc", true, "按时间降序排序（从新到旧）")
			byName := exportCmd.Bool("byname", false, "在文件名前添加序号")
			exportCmd.Parse(os.Args[3:])

			// 获取数据库路径
			if *dbPath == "" {
				*dbPath = getDefaultDBPath()
				if *dbPath == "" {
					if *jsonOutput {
						errMsg := "无法确定默认数据库路径"
						response := ExportResponse{
							Success:  false,
							Exported: nil,
							Total:    0,
							Error:    &errMsg,
						}
						jsonData, _ := json.MarshalIndent(response, "", "  ")
						fmt.Println(string(jsonData))
						return
					}
					fmt.Println("无法确定默认数据库路径")
					return
				}
			}

			if err := exportSingleSession(*dbPath, *outputDir, hash, *jsonOutput, *sortDesc, *byName); err != nil {
				if *jsonOutput {
					errMsg := err.Error()
					response := ExportResponse{
						Success:  false,
						Exported: nil,
						Total:    0,
						Error:    &errMsg,
					}
					jsonData, _ := json.MarshalIndent(response, "", "  ")
					fmt.Println(string(jsonData))
					return
				}
				fmt.Printf("导出会话失败: %v\n", err)
			}
			return
		}

		// 原有的批量导出逻辑
		var config Config
		exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
		exportCmd.StringVar(&config.DBPath, "db", "", "数据库文件路径 (默认: 系统默认路径)")
		exportCmd.StringVar(&config.OutputDir, "out", "markdown_output", "markdown文件输出目录")
		exportCmd.BoolVar(&config.JsonOutput, "json", false, "以JSON格式输出")
		exportCmd.BoolVar(&config.SortDesc, "sort-desc", true, "按时间降序排序（从新到旧）")
		exportCmd.BoolVar(&config.ByName, "byname", false, "在文件名前添加序号")
		var startAfterStr, startBeforeStr, endAfterStr, endBeforeStr string
		exportCmd.StringVar(&startAfterStr, "start-after", "", "仅包含在此时间之后开始的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
		exportCmd.StringVar(&startBeforeStr, "start-before", "", "仅包含在此时间之前开始的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
		exportCmd.StringVar(&endAfterStr, "end-after", "", "仅包含在此时间之后结束的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")
		exportCmd.StringVar(&endBeforeStr, "end-before", "", "仅包含在此时间之前结束的会话 (格式: 2006-01-02 或 2006-01-02 15:04:05)")

		exportCmd.Parse(os.Args[2:])

		var err error
		if config.StartAfter, err = parseTimeArg(startAfterStr); err != nil {
			if config.JsonOutput {
				errMsg := fmt.Sprintf("解析start-after参数失败: %v", err)
				response := ExportResponse{
					Success:  false,
					Exported: nil,
					Total:    0,
					Error:    &errMsg,
				}
				jsonData, _ := json.MarshalIndent(response, "", "  ")
				fmt.Println(string(jsonData))
				return
			}
			fmt.Printf("解析start-after参数失败: %v\n", err)
			return
		}
		if config.StartBefore, err = parseTimeArg(startBeforeStr); err != nil {
			if config.JsonOutput {
				errMsg := fmt.Sprintf("解析start-before参数失败: %v", err)
				response := ExportResponse{
					Success:  false,
					Exported: nil,
					Total:    0,
					Error:    &errMsg,
				}
				jsonData, _ := json.MarshalIndent(response, "", "  ")
				fmt.Println(string(jsonData))
				return
			}
			fmt.Printf("解析start-before参数失败: %v\n", err)
			return
		}
		if config.EndAfter, err = parseTimeArg(endAfterStr); err != nil {
			if config.JsonOutput {
				errMsg := fmt.Sprintf("解析end-after参数失败: %v", err)
				response := ExportResponse{
					Success:  false,
					Exported: nil,
					Total:    0,
					Error:    &errMsg,
				}
				jsonData, _ := json.MarshalIndent(response, "", "  ")
				fmt.Println(string(jsonData))
				return
			}
			fmt.Printf("解析end-after参数失败: %v\n", err)
			return
		}
		if config.EndBefore, err = parseTimeArg(endBeforeStr); err != nil {
			if config.JsonOutput {
				errMsg := fmt.Sprintf("解析end-before参数失败: %v", err)
				response := ExportResponse{
					Success:  false,
					Exported: nil,
					Total:    0,
					Error:    &errMsg,
				}
				jsonData, _ := json.MarshalIndent(response, "", "  ")
				fmt.Println(string(jsonData))
				return
			}
			fmt.Printf("解析end-before参数失败: %v\n", err)
			return
		}

		config.HasTimeFilter = !config.StartAfter.IsZero() || !config.StartBefore.IsZero() ||
			!config.EndAfter.IsZero() || !config.EndBefore.IsZero()

		if config.DBPath == "" {
			config.DBPath = getDefaultDBPath()
			if config.DBPath == "" {
				fmt.Println("无法确定默认数据库路径")
				return
			}
		}

		if err := exportSessions(config); err != nil {
			if config.JsonOutput {
				errMsg := err.Error()
				response := ExportResponse{
					Success:  false,
					Exported: nil,
					Total:    0,
					Error:    &errMsg,
				}
				jsonData, _ := json.MarshalIndent(response, "", "  ")
				fmt.Println(string(jsonData))
				return
			}
			fmt.Printf("导出会话失败: %v\n", err)
		} else if !config.JsonOutput {
			fmt.Println("导出完成!")
		}

	case "version":
		jsonOutput := false
		versionCmd := flag.NewFlagSet("version", flag.ExitOnError)
		versionCmd.BoolVar(&jsonOutput, "json", false, "以JSON格式输出")
		versionCmd.Parse(os.Args[2:])

		if jsonOutput {
			response := VersionResponse{
				Version: "0.0.2",
				Success: true,
			}
			jsonData, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(jsonData))
		} else {
			fmt.Println("cursor2md version 0.0.2")
		}

	case "help":
		printHelp()

	default:
		printHelp()
	}
}

func printHelp() {
	fmt.Println("使用说明:")
	fmt.Println("  cursor2md ls [-db <数据库路径>] [-json]  列出所有会话信息")
	fmt.Println("  cursor2md export [<hash>] [-db <数据库路径>] [-out <输出目录>] [-sort-desc] [-byname]  导出指定hash的会话")
	fmt.Println("  cursor2md export [-db <数据库路径>] [-out <输出目录>] [-sort-desc] [-byname] [-start-after <时间>] [-start-before <时间>] [-end-after <时间>] [-end-before <时间>]  导出会话记录")
	fmt.Println("  cursor2md version  显示版本信息")
	fmt.Println("  cursor2md help  显示此帮助信息")
	fmt.Println("\n排序参数说明:")
	fmt.Println("                使用-sort-desc=false可改为升序排序（从旧到新）")
	fmt.Println("  -byname      在文件名前添加序号（例如：001-文件名.md）")
}
