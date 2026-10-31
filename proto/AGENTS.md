# Protobuf 候选

这是独立 M0-P1-r1 候选，不是冻结生产协议。原文在 docs/reference；只新增 Go 包路径和格式修订。字段号/oneof/enum 的变更须说明 wire、生成源码和跨版本影响。契约负责人负责，相关 transport/tasks/auth owner 审查。先 `npm run generate`，再 `npm run check:contracts`；禁止手改 gen/go。兼容基线更新须单独批准，不能为使 breaking 检查通过而直接同步改基线。
