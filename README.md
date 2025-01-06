# Cursor聊天记录提取工具

这是一个适用于**cursor 0.43版本以后**的Cursor聊天记录提取工具，代码使用cursor自动生成，主要用于将Cursor编辑器中的AI聊天记录提取并转换为Markdown文件的工具。



## 功能特点

- 查看功能：列出所有AI聊天记录的基本信息
- 导出功能：将聊天记录转换为Markdown文件
- 支持时间范围筛选
- 自动过滤空的或无效的聊天记录
- 支持跨平台（Windows、macOS、Linux）
- 支持自定义数据库文件路径和输出目录

## 安装依赖

本程序依赖sqlite3驱动，请先安装：

```shell
go get github.com/mattn/go-sqlite3
```

## 编译

通常您安装sqlite3的依赖后直接使用如下命令即可编译生成可执行文件：

```shell
go build cursor2md.go
```

在Windows平台，因为[go-sqlite3](github.com/mattn/go-sqlite3)的编译需要，您需要额外做如下设置：

1. 允许golang进行cgo编译：
   ```shell
   go env -w CGO_ENABLED=1
   ```

2. 安装[gcc](https://jmeubank.github.io/tdm-gcc/)

## 使用方法

### 查看聊天记录列表

```shell
# 使用默认数据库路径
./cursor2md ls

# 指定数据库路径
./cursor2md ls -db path/to/state.vscdb
```

### 导出聊天记录

```shell
# 导出指定hash的会话记录
./cursor2md export 48c9b7a2-b3fe-4428-bdfd-b4d7ede0b26d

# 导出全部聊天记录（使用默认路径）
./cursor2md export

# 指定数据库路径和输出目录
./cursor2md export -db path/to/state.vscdb -out path/to/output

# 导出指定时间范围的记录
./cursor2md export -start-after "2024-01-01" -end-before "2024-02-01"

# 组合使用多个参数
./cursor2md export -db path/to/state.vscdb -out path/to/output -start-after "2024-01-01"
```

### 其他命令

```shell
# 显示帮助信息
./cursor2md help

# 显示版本信息
./cursor2md version
```

### 默认路径说明

数据库文件默认位置：
- Windows: `%APPDATA%/Cursor/User/globalStorage/state.vscdb`
- macOS: `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
- Linux: `~/.config/Cursor/User/workspaceStorage/state.vscdb`

## 输出说明

- 所有生成的Markdown文件将保存在指定的输出目录（默认为`markdown_output`）
- 文件名使用聊天记录的标题
- 每个文件包含完整的对话内容，包括：
    - 会话信息（开始时间、结束时间、相关文件）
    - 用户输入（包含引用的文件和代码片段）
    - AI回复（包含代码示例）

## 注意事项

1. 确保有足够的磁盘空间存储转换后的文件
2. 程序会自动跳过空的或无效的聊天记录
3. 时间参数支持以下格式：
    - `YYYY-MM-DD`
    - `YYYY-MM-DD HH:mm`
    - `YYYY-MM-DD HH:mm:ss`

## 错误处理

程序会在遇到以下情况时提供错误信息：
- 数据库文件不存在或无法访问
- 创建输出目录失败
- 解析JSON数据失败
- 写入Markdown文件失败
- 时间参数格式错误

## 许可证

MIT License