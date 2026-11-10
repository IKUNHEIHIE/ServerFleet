import { useState } from 'react'
import { useSession } from '@/app/session'
import { apiClient } from '@/lib/api/client'

// Uses the same authenticated REST/CSRF adapter as the surrounding settings.
export function ResidentUpdatePanel() {
  const session=useSession()
  const allowed=session.can('system.update') || session.can('settings.manage') || session.can('update.manage')
  const [uncertain,setUncertain]=useState(false)
  const post=async(path:string,body:unknown)=>(await apiClient.request<unknown>(path,{method:'POST',body})).data
  const get=async(path:string)=>(await apiClient.request<unknown>(path)).data
  const [component, setComponent] = useState('agent')
  const [nodes, setNodes] = useState('')
  const [version, setVersion] = useState('')
  const [keyId, setKeyId] = useState('')
  const [releaseSha256, setReleaseSHA] = useState('')
  const [policySha256, setPolicySHA] = useState('')
  const [taskId, setTask] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit() {
    setBusy(true)
    try {
      const common = { version, keyId, releaseSha256, policySha256, idempotencyKey: crypto.randomUUID() }
      const result = await post(component === 'agent' ? '/api/v1/system/agent-fleet-updates' : '/api/v1/system/controller-update-tasks', component === 'agent' ? { ...common, nodeIds: nodes.split(/[\s,]+/).filter(Boolean) } : common) as { taskId?: string; status?: string }
      setTask(result.taskId ?? '')
      setMessage(`已登记 ${result.status ?? ''}，需要独立真人管理员审批；这不代表已安装。`)
    } catch { setUncertain(true); setMessage('更新请求被拒绝或结果未确认，请先检查任务记录，勿重复提交未知执行。') }
    finally { setBusy(false) }
  }
  async function receipt() {
    setBusy(true)
    try { setMessage(JSON.stringify(await get(`/api/v1/system/controller-update-receipts/${encodeURIComponent(taskId)}`), null, 2)) }
    catch { setMessage('尚无可核验的 Controller 安装回执；保持 UNKNOWN，勿重跑。') }
    finally { setBusy(false) }
  }
  return <section aria-label="受监督的签名更新">
    <h3>签名组件更新</h3>
    {!allowed && <p role="alert">需要管理员更新权限。</p>}
    <p>仅支持已配置独立监督进程的 Linux 主机。Agent 先 1 台再每批最多 5 台，非成功即停止扩批；不同架构或不同策略应分开审批。Controller 重启后的任务 UNKNOWN 与安装回执分别展示。</p>
    <label>组件<select value={component} onChange={e => setComponent(e.target.value)}><option value="agent">远程 Agent 批次</option><option value="controller">Controller 自身</option></select></label>
    {component === 'agent' && <label>按灰度顺序的节点 ID<input value={nodes} onChange={e => setNodes(e.target.value)} /></label>}
    <label>签名版本<input value={version} onChange={e => setVersion(e.target.value)} /></label>
    <label>已配置信任 Key ID<input value={keyId} onChange={e => setKeyId(e.target.value)} /></label>
    <label>发行文件 SHA-256<input value={releaseSha256} onChange={e => setReleaseSHA(e.target.value)} /></label>
    <label>监督策略 SHA-256<input value={policySha256} onChange={e => setPolicySHA(e.target.value)} /></label>
    <button type="button" disabled={busy || uncertain || session.writesLocked || !allowed} onClick={() => void submit()}>创建人工审批更新任务</button>
    <label>任务 ID<input value={taskId} onChange={e => setTask(e.target.value)} /></label>
    {taskId && <a href="#/tasks">查看任务与逐节点结果</a>}
    {component === 'controller' && <button type="button" disabled={busy || !taskId} onClick={() => void receipt()}>读取历史安装回执</button>}
    <pre role="status">{message}</pre>
  </section>
}
