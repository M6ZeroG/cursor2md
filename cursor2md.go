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

	_ "github.com/mattn/go-sqlite3"
)

// 定义JSON结构体
type ChatRecord struct {
	Conversation []Message `json:"conversation"`
	Name         string    `json:"name"`
}

type Message struct {
	Type       int    `json:"type"`
	Text       string `json:"text"`
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
			fmt.Printf("读取记录失败: %v\n", err)
			continue
		}

		// 解析JSON
		var record ChatRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			fmt.Printf("解析JSON失败 (key=%s): %v\n", key, err)
			continue
		}

		// 如果name为空，使用key作为文件名
		if record.Name == "" {
			record.Name = key
		}

		// 检查是否有实际内容
		if !hasValidContent(record) {
			fmt.Printf("跳过空记录: %s\n", record.Name)
			continue
		}

		// 生成markdown内容
		mdContent := convertToMarkdown(record)

		// 创建markdown文件
		mdFile := filepath.Join(outputDir, record.Name+".md")
		if err := ioutil.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
			fmt.Printf("写入markdown文件 %s 失败: %v\n", mdFile, err)
			continue
		}

		fmt.Printf("成功生成markdown文件: %s\n", mdFile)
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
	md.WriteString(fmt.Sprintf("# %s\n\n", record.Name))

	for _, msg := range record.Conversation {
		switch msg.Type {
		case 1: // 用户消息
			md.WriteString("## User\n\n")
			md.WriteString(msg.Text + "\n\n")
		case 2: // AI回复
			md.WriteString("## Cursor\n\n")
			md.WriteString(msg.Text + "\n\n")
			// 处理代码块
			for _, block := range msg.CodeBlocks {
				if block.Content != "" {
					if block.Uri.Path != "" {
						md.WriteString(fmt.Sprintf("```%s:%s\n", block.LanguageId, block.Uri.Path))
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
