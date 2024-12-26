# Cursor聊天记录提取工具

这是一个适用于**cursor 0.43版本以后**的Cursor聊天记录提取工具，代码使用cursor自动生成，主要用于将Cursor编辑器中的AI聊天记录提取并转换为Markdown文件的工具。



## 功能特点

- 从Cursor本地存储数据库中提取AI聊天记录
- 将每条聊天记录转换为独立的Markdown文件
- 自动过滤空的或无效的聊天记录
- 支持跨平台（Windows、macOS、Linux）
- 支持自定义数据库文件路径



## 安装依赖

本程序依赖sqlite3驱动，请先安装： 

```shell
go get github.com/mattn/go-sqlite3
```



## 编译

通常您安装sqlite3的依赖后直接使用如下命令即可编译生成可执行文件

```shell
go build cursor2md.go
```

在windows平台，因为[go-sqlite3](github.com/mattn/go-sqlite3)的编译需要，您需要额外做如下设置后再使用上述命令进行编译：

- 允许golang进行cgo编译

  ```shell
  go env -w CGO_ENABLED=1
  ```

- 安装[gcc](https://jmeubank.github.io/tdm-gcc/)



## 使用方法

1. 直接使用默认路径：

   ```shell
   ./cursor2md
   ```

默认的数据库文件位置：
- Windows: `%APPDATA%/Cursor/User/globalStorage/state.vscdb`
- macOS: `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
- Linux: `~/.config/Cursor/User/workspaceStorage/state.vscdb`

2. 指定数据库文件路径：

   ```shell
   ./cursor2md path/to/state.vscdb
   ```



## 输出说明

- 所有生成的Markdown文件将保存在`markdown_output`目录下
- 文件名使用聊天记录的标题或ID
- 每个文件包含完整的对话内容，包括：
  - 用户输入
  - AI回复
  - 代码示例（带有语言标识和文件路径）



## 注意事项

1. 确保有足够的磁盘空间存储转换后的文件
2. 程序会自动跳过空的或无效的聊天记录
3. 如果输出目录已存在，会直接在其中创建新文件



## 错误处理

程序会在遇到以下情况时提供错误信息：
- 数据库文件不存在或无法访问
- 创建输出目录失败
- 解析JSON数据失败
- 写入Markdown文件失败



## 许可证

MIT License