import { useState } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Field, TextInput, TextArea } from "@/components/ui/form";
import { Notice } from "@/components/ui/feedback";
import { apiClient } from "@/lib/api/client";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";

const ids = (v: string) => v.split(/[\s,，]+/).filter(Boolean);
const validIds = (v: readonly string[], max: number) => v.length <= max && new Set(v).size === v.length && v.every(x => /^[a-zA-Z0-9_-]{1,128}$/.test(x));
export function RenewalRecoveryPanel({ policyId, policyRevision, deliveryRevision, onChanged }: { readonly policyId: string; readonly policyRevision: number; readonly deliveryRevision: number; readonly onChanged: () => void }) {
 const session = useSession();
 const [order, setOrder] = useState(""); const [install, setInstall] = useState(""); const [publications, setPublications] = useState(""); const [incidents, setIncidents] = useState(""); const [note, setNote] = useState("");
 const [confirmed, setConfirmed] = useState(false); const [enabled, setEnabled] = useState(false);
 const recover = useAction((body: object) => apiClient.request("/api/v1/certificates/public-renewals/" + encodeURIComponent(policyId) + "/delivery/recover", { method: "POST", body }));
 const pubs = ids(publications); const cases = ids(incidents);
 const valid = order !== "" && install !== "" && validIds([order], 1) && validIds([install], 1) && validIds(pubs, 8) && validIds(cases, 66) && note.trim().length > 0 && new TextEncoder().encode(note).length <= 4096 && confirmed;
 return <details className="space-y-3 rounded border p-3"><summary>人工修复后采用成功结果</summary>
  <p>先通过现有任务和发布入口完成独立修复。所有原任务必须已终止；每项 UNKNOWN 须在任务中心完成人工处置与独立复核。采用操作不会重试原任务。</p>
  <Field label="修复后成功签发任务 ID">{a => <TextInput id={a.inputId} value={order} onChange={e => setOrder(e.target.value)} />}</Field>
  <Field label="修复后成功安装任务 ID">{a => <TextInput id={a.inputId} value={install} onChange={e => setInstall(e.target.value)} />}</Field>
  <Field label="修复后完成的发布计划 ID" hint="最多8个；须使用上述新安装证书。留空表示后续仅自动安装，原站点需另行确认。">{a => <TextArea id={a.inputId} value={publications} onChange={e => setPublications(e.target.value)} />}</Field>
  <Field label="已独立复核结案的 UNKNOWN 处置单 ID" hint="每行一个，须覆盖本周期全部 UNKNOWN 任务；没有 UNKNOWN 时留空。">{a => <TextArea id={a.inputId} value={incidents} onChange={e => setIncidents(e.target.value)} />}</Field>
  <Field label="恢复依据" hint="说明核验与修复结果，不填写令牌、密码或私钥。">{a => <TextArea id={a.inputId} value={note} onChange={e => setNote(e.target.value)} />}</Field>
  <label className="flex gap-2"><input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} />采用后启用未来周期的安装发布编排</label>
  <label className="flex gap-2"><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />我已核对新签发、安装与站点状态，确认采用这些成功结果</label>
  <Button loading={recover.pending} disabled={session.writesLocked || recover.pending || !valid} onClick={async () => {
   const result = await recover.run({ expectedRevision: deliveryRevision, policyRevision, orderTaskId: order, installTaskId: install, publicationIds: pubs, incidentIds: cases, note: note.trim(), confirmed, enabled });
   setConfirmed(false); onChanged(); if (result.ok) { setOrder(""); setInstall(""); setNote(""); }
  }}>确认采用修复结果</Button>
  {recover.error ? <Notice title="恢复未确认" tone="error">{describeApiError(recover.error)}。已重新读取版本，请核对当前证据与处置状态。</Notice> : null}
 </details>;
}
