import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import type { NodeDetail } from "@/lib/api/dto";
import { nodesApi } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";

export function NodeEditor({
  nodeId, metadata, canManage, writesLocked, onClose, onSaved, onRefresh,
}: {
  readonly nodeId: string;
  readonly metadata: NodeDetail;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: () => void;
  readonly onRefresh: () => void;
}) {
  // Freeze the metadata and CAS token together for this editing session.
  const [snapshot] = useState(metadata);
  const [name, setName] = useState(snapshot.displayName);
  const [address, setAddress] = useState(snapshot.address ?? "");
  const [region, setRegion] = useState(snapshot.region ?? "");
  const [provider, setProvider] = useState(snapshot.provider ?? "");
  const [countryCode, setCountryCode] = useState(snapshot.countryCode ?? "");
  const [tags, setTags] = useState(snapshot.tags?.join(", ") ?? "");
  const action = useAction((body: Parameters<typeof nodesApi.update>[1]) => nodesApi.update(nodeId, body));
  const expectedUpdatedAt = typeof snapshot.updatedAt === "string" && snapshot.updatedAt.trim() ? snapshot.updatedAt : null;
  const conflicted = action.error?.kind === "conflict" || action.error?.kind === "precondition";
  const locked = !canManage || writesLocked || !expectedUpdatedAt || conflicted;
  const validCountry = !countryCode.trim() || /^[a-z]{2}$/i.test(countryCode.trim());

  async function save() {
    if (locked || action.pending || !expectedUpdatedAt || !name.trim() || !validCountry) return;
    const result = await action.run({
      name: name.trim(),
      address: address.trim(),
      region: region.trim(),
      provider: provider.trim(),
      countryCode: countryCode.trim().toUpperCase(),
      tags: [...new Set(tags.split(/[,，]/).map(tag => tag.trim()).filter(Boolean))],
      expectedUpdatedAt,
    });
    if (result.ok) onSaved();
  }

  return (
    <Modal
      open
      title="编辑节点"
      description="修改节点名称与资产信息；连接状态由节点实际接入情况决定。"
      onClose={() => { if (!action.pending) onClose(); }}
      footer={<>
        <Button type="button" disabled={action.pending} onClick={onClose}>取消</Button>
        <Button type="button" variant="primary" loading={action.pending} disabled={locked || !name.trim() || !validCountry} onClick={save}>保存节点</Button>
      </>}
    >
      <div className="space-y-4">
        {!expectedUpdatedAt || conflicted ? (
          <Notice tone="warning" title="请刷新节点信息">
            {!expectedUpdatedAt ? "当前信息缺少更新时间，暂时无法保存。请刷新后重新编辑。" : "节点信息已变化，请刷新后重新编辑。"}
            <Button type="button" className="mt-2" disabled={action.pending} onClick={onRefresh}>刷新节点信息</Button>
          </Notice>
        ) : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <fieldset disabled={locked || action.pending} className="grid gap-4 sm:grid-cols-2">
          <Field label="名称">{ids => <TextInput id={ids.inputId} value={name} required onChange={event => setName(event.target.value)} />}</Field>
          <Field label="地址">{ids => <TextInput id={ids.inputId} value={address} onChange={event => setAddress(event.target.value)} />}</Field>
          <Field label="地区">{ids => <TextInput id={ids.inputId} value={region} onChange={event => setRegion(event.target.value)} />}</Field>
          <Field label="云厂商">{ids => <TextInput id={ids.inputId} value={provider} onChange={event => setProvider(event.target.value)} />}</Field>
          <Field label="国家码" hint="两位字母，例如 CN、JP。" error={validCountry ? null : "请输入两位字母国家码。"}>{ids => <TextInput id={ids.inputId} aria-describedby={ids.hintId} aria-invalid={!validCountry} value={countryCode} onChange={event => setCountryCode(event.target.value)} />}</Field>
          <Field label="标签" hint="多个标签以逗号分隔。">{ids => <TextInput id={ids.inputId} aria-describedby={ids.hintId} value={tags} onChange={event => setTags(event.target.value)} />}</Field>
        </fieldset>
      </div>
    </Modal>
  );
}
