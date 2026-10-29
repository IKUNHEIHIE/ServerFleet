# internal/audit 交接说明（A01）

范围：事件字段校验、受限只读投影、测试内存 Recorder。未接数据库、HTTP、生产日志、保留期或防篡改存储。

## 不变量

- `Event` 字段固定为 `SubjectID`、`Action`、`Result`、`RequestID`、`At`，不增加 payload/message/details/error 等自由容器。
- `Validator` 拒绝空值、超长、非法 UTF-8、控制字符（含 Unicode 格式字符与行/段分隔符）、首尾空白、未知 action/result 和越界时间。Action 允许列表由调用方显式提供且必须是可信当前目录；Result 仅接受 `allowed/denied/success/failure/partial`。长度、未来偏移和最大历史跨度通过 `Limits` 注入，`Validate(event, now)` 显式传入参考时间。
- 校验错误只暴露字段名和原因码，不回显被拒绝的值；这不等于能识别任意字符串中的全部秘密。
- `EventView` 仅含上述五个明确字段。`NewView` 复制快照；`Query` 按字段精确过滤，`Since` 含、`Until` 不含，按时间倒序、相同时间保持追加顺序，offset/limit 只读取不修改记录。默认 50/最大 200 是本模块临时分页策略，不是 G2 冻结值。
- 只读视图不执行权限过滤；调用方必须先完成读取授权。读取时不重新校验历史 action，避免因允许列表演进而丢弃旧事件。
- `testkit.MemoryRecorder` 仅用于测试：追加前校验，ctx 已取消或写入前取消则不写入，快照为副本，相同 RequestID 或相同内容均保留多条，并用互斥锁支持并发写入；注入的时钟函数须自身并发安全。它不是持久、防篡改或真实审计日志，禁止接入生产组合根。

## 未完成 / 后续

- 持久化、写入事务、保留/删除策略、导出、外部防篡改、真实审计完整性均未实现。
- 读取权限过滤、按用户可见范围裁剪、敏感信息二次识别需由后续存储/授权/安全评审提供。
- 真实 action 目录与分页契约尚未冻结；当前由调用方显式注入，不能当作全局公共契约。

## 验证

`go test -count=1 -cover ./internal/audit/...`、`npm run check:go`、`npm run check:policy`。
