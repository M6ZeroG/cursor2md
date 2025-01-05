package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
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

// 处理单个数据库文件
func processDatabase(dbPath string) error {
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
	outputDir := "markdown_output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
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

		// 生成markdown内容
		mdContent := convertToMarkdown(record)

		// 创建markdown文件
		mdFile := filepath.Join(outputDir, record.Name+".md")
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

func main() {
	var dbPath string

	// 检查命令行参数
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	} else {
		// 使用默认路径
		dbPath = getDefaultDBPath()
		if dbPath == "" {
			fmt.Println("无法确定默认数据库路径")
			return
		}
	}

	// 处理数据库文件
	if err := processDatabase(dbPath); err != nil {
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
