import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi } from "@/lib/api/endpoints";
import type { TrafficQuota } from "@/lib/api/dto";
import { useAction } from "@/lib/data/use-resource";

export function NodeQuotaEditor({
  nodeId,
  quota,
  open,
  canManage,
  writesLocked,
  onClose,
  onSaved,
}: {
  readonly nodeId: string;
  readonly quota: TrafficQuota | null;
  readonly open: boolean;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: () => void;
}) {
  const [quotaGb, setQuotaGb] = useState("");
  const [cycleResetDay, setCycleResetDay] = useState("1");
  const [timezone, setTimezone] = useState("UTC");
  const [direction, setDirection] = useState<TrafficQuota["direction"]>("combined");
  const [thresholds, setThresholds] = useState("70,85,95");
  const [expiryType, setExpiryType] = useState<TrafficQuota["expiryType"] | "">("");
  const [expiresAt, setExpiresAt] = useState("");
  const [remindDays, setRemindDays] = useState("30,7,1");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: TrafficQuota) => nodesApi.setTrafficQuota(nodeId, body));

  useEffect(() => {
    if (!open || !quota) return;
    setQuotaGb(String(quota.quotaGb));
    setCycleResetDay(quota.cycleResetDay === undefined ? "1" : String(quota.cycleResetDay));
    setTimezone(quota.timezone ?? "UTC");
    setDirection(quota.direction);
    setThresholds(quota.thresholdsPct?.join(",") ?? "70,85,95");
    setExpiryType(quota.expiryType ?? "");
    const expiry=quota.expiresAt ? new Date(quota.expiresAt) : null;
    setExpiresAt(expiry && !Number.isNaN(expiry.getTime()) ? new Date(expiry.getTime()-expiry.getTimezoneOffset()*60_000).toISOString().slice(0,16) : "");
    setRemindDays(quota.remindDays?.join(",") ?? "30,7,1");
    setLocalError(null);
  }, [open, quota]);

  async function save() {
    const numericQuota = Number(quotaGb);
    if (!Number.isFinite(numericQuota) || numericQuota <= 0) return setLocalError("配额必须是大于 0 的数字。");
    const resetDay = Number(cycleResetDay);
    if (!Number.isInteger(resetDay) || resetDay < 1 || resetDay > 28) return setLocalError("重置日必须在 1–28。");
    const thresholdList = thresholds.split(",").map(value => Number(value.trim())).filter(value => Number.isFinite(value));
    const remindList = remindDays.split(",").map(value => Number(value.trim())).filter(value => Number.isFinite(value));
    const expiry = expiresAt ? new Date(expiresAt) : null;
    if (expiry && Number.isNaN(expiry.getTime())) return setLocalError("到期时间无法解析。");
    setLocalError(null);
    const result = await action.run({
      nodeId,
      quotaGb: numericQuota,
      cycleResetDay: resetDay,
      timezone: timezone.trim() || "UTC",
      direction,
      thresholdsPct: thresholdList,
      remindDays: remindList,
      ...(expiryType ? { expiryType } : {}),
      ...(expiry ? { expiresAt: expiry.toISOString() } : {}),
    });
    if (result.ok) onSaved();
  }

  return (
    <Modal
      open={open}
      title="编辑流量配额与到期提醒"
      description="PUT /api/v1/nodes/{nodeId}/traffic-quota；估算值与账单分开，不自动断网、续费或购买流量。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="button" variant="primary" onClick={save} loading={action.pending} disabled={!canManage || writesLocked}>保存配额</Button>
        </>
      }
    >
      <div className="space-y-3">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="配额 (GB)">{ids => <TextInput id={ids.inputId} type="number" min={0.01} step="0.01" value={quotaGb} onChange={event => setQuotaGb(event.target.value)} />}</Field>
          <Field label="周期重置日（1–28）">{ids => <TextInput id={ids.inputId} type="number" min={1} max={28} value={cycleResetDay} onChange={event => setCycleResetDay(event.target.value)} />}</Field>
          <Field label="时区">{ids => <TextInput id={ids.inputId} value={timezone} onChange={event => setTimezone(event.target.value)} />}</Field>
          <Field label="计费方向">
            {ids => (
              <Select id={ids.inputId} value={direction} onChange={event => setDirection(event.target.value as TrafficQuota["direction"])}>
                <option value="ingress">入站</option>
                <option value="egress">出站</option>
                <option value="combined">合计</option>
              </Select>
            )}
          </Field>
          <Field label="告警阈值 %（逗号分隔）">{ids => <TextInput id={ids.inputId} value={thresholds} onChange={event => setThresholds(event.target.value)} />}</Field>
          <Field label="到期类型">
            {ids => (
              <Select id={ids.inputId} value={expiryType} onChange={event => setExpiryType(event.target.value as TrafficQuota["expiryType"] | "")}>
                <option value="">未设置</option>
                <option value="instance">实例到期</option>
                <option value="trial">试用结束</option>
                <option value="credit">额度结束</option>
                <option value="other">其他</option>
              </Select>
            )}
          </Field>
          <Field label="到期时间（本地时间，可选）">{ids => <TextInput id={ids.inputId} type="datetime-local" value={expiresAt} onChange={event => setExpiresAt(event.target.value)} />}</Field>
          <Field label="提醒天数（逗号分隔）">{ids => <TextInput id={ids.inputId} value={remindDays} onChange={event => setRemindDays(event.target.value)} />}</Field>
        </div>
        <p className="text-xs text-muted-foreground">默认仅提醒；数据缺失不按 0 处理。重置日与时区用于月度统计；阈值须升序，提醒天数须降序。Agent 统计仅累计实际观测到的流量，不补算安装前或离线期间数据；不能通过此处选择供应商账单。</p>
      </div>
    </Modal>
  );
}
