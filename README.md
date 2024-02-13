- 使用 slog 作为日志库。对于 Go 1.21 以后版本的项目，slog 内置在标准库中。对于之前版本，需要 引入 `exp/slog` 来使用（TODO: 日志持久化）
- 修改为 Golang 官方标准项目结构 
- 使用 Swagger 生成接口文档（TODO: 给每个接口完善注释，而且感觉 go swagger 并不好用，寻找更好的方案）
- 修改全局变量的使用为依赖注入形式, 统一在 main.go 初始化
- 修改 utils 包架构, 精简项目架构
- 开发环境使用 sqlite 进行单元测试, 线上支持 MySQL 环境
- 移除 Casbin，自己实现基于 RBAC 的权限控制
- 之前版本错误码使用 panic 机制, 使用 gin 中间件全局捕获 panic
- TODO: 使用 errors 机制来统一错误码机制

新版本项目运行：运行/初始化数据 相关文件都在 cmd 下

1、MySQL，`config.yml` 中 `DbType = "mysql"` 并修改 MySQL 连接信息， 导入 `assets/gvb.sql`（也可以手动初始化数据，参考 SQLite 的情况）

2、SQLite，`config.yml` 中 `DbType = "sqlite"`，并执行初始化数据操作：
- `create_superadmin.sh` 创建一个超级管理员（拥有所有权限），用户名 superadmin 密码 superadmin
- `generate_data.sh` 初始化 三个默认角色 (admin, user, guest) + 三个默认用户 (admin, user, guest, 密码都是 123456),  初始化配置数据, 初始化页面数据, 初始化资源数据 (TODO: )

