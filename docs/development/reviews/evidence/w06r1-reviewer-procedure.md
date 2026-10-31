# 总控3项独立浏览器补验的复核步骤

实际工具：Computer Use cua_repl，后台新IAB页，页面127.0.0.1:8463/index.html。Node24.22.1；源码HEAD ed6b80c仅加任务登记，生产组件同157549e。预览3文件从作者w06r1复制到本任务reports/_review/w06close后，执行`npx vite build --config reports/_review/w06close/vite.preview.config.ts`，再将输出预览HTML的../../assets/转为./assets/并保存dist/index.html。预览来源及构建日志已归档，未改生产工程。

服务由`node reports/_review/w06close/serve.mjs`启动，仅127.0.0.1:8463。新tab.goto后获取tab.capabilities.get("cdp")；Runtime.evaluate仅调用这个明确隔离的QA接缝，不用终端/CDP自制客户端绕开Computer Use。每步变更后读取AX状态；记录请求数组、snapshot、focus和errors。结构化记录含实际11步值，body为预览原有最多500字符，不冒充完整页面文本；sourceKind捕获初值，不用于推断当前key。

实际顺序（请求序号来自每一步已读的requests数组）：

1. 初始A request0 pending；setSource('broken')，观察alert，request0.abort=true。
2. setSource('a')恢复，观察loading，新request1；检查request0/request1 key均scope-a，0已abort。
3. settle(0,'failure')；实际观察仍loading、无table/alert/detail/errors，保存截图。随后settle(1,'success')，观察8个节点。
4. setSource('b')产生request2，核对pending；setMounted(false)只卸载组件。实际检查2已abort且未settle，再settle(2,'success')；内容不复活、无错误。
5. setMounted(true)产生独立request3，核对pending且未abort；setMounted(false)，再实际检查3已abort；settle(3,'failure')。仍无table/alert/detail，errors=[]、focus=BODY，保存截图。
6. 原样保存trace为supplemental-results.json，结果3项PASS、11步、错误0。关闭创建的tab，仅中断本任务静态服务，确认8463无监听。

不是25项作者浏览器套件的重跑，也未执行键盘激活补验；同ID/关闭按钮的键盘证据独立核验作者C20–C22。不存在真实登录/凭据/HTTP数据源或跨主体测试。本文件是实际动作复核说明，不冒充另一个执行过的自动化脚本。
