import { useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { AgentResumePanel } from "./agent-resume-panel";
import { FileDistributionPanel } from "./file-distribution-panel";
import { FilePane, INITIAL_PANE, type PaneState } from "./file-pane";
import { TransferHistoryModal } from "./transfer-history";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { PageHeader, SectionDivider } from "@/components/ui/panel";
import { nodesApi } from "@/lib/api/endpoints";
import { useResource } from "@/lib/data/use-resource";

export function FilesPage() {
  const session = useSession();
  const [paneA, setPaneA] = useState<PaneState>(INITIAL_PANE);
  const [paneB, setPaneB] = useState<PaneState>(INITIAL_PANE);
  const [historyOpen, setHistoryOpen] = useState(false);

  const nodes = useResource(`files:nodes:${session.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];

  useEffect(() => {
    if (nodeItems.length === 0) return;
    setPaneA(current => current.nodeId ? current : { ...current, nodeId: nodeItems[0].id });
    setPaneB(current => current.nodeId ? current : { ...current, nodeId: (nodeItems[1] ?? nodeItems[0]).id });
  }, [nodeItems]);

  return (
    <div className="space-y-5">
      <PageHeader
        title="文件互传"
        description="双栏独立浏览目录，支持跨节点复制；符号链接默认不跟随。"
        badge={<Badge tone="primary" dot>异步复制</Badge>}
        actions={
          <>
            <Button type="button" onClick={() => setHistoryOpen(true)}>传输任务</Button>
            <Button type="button" onClick={() => nodes.reload()}><RefreshCw className="size-3.5" />刷新</Button>
          </>
        }
      />
      <Notice tone="info" title="说明">
        上传会校验文件完整性；界面区分“校验通过”与“未送达远端”。暂不支持下载。
      </Notice>

      <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="节点列表加载失败" empty={<EmptyState title="没有可用节点" />} isEmpty={data => data.items.length === 0}>
        {() => (
          <div className="grid gap-4 xl:grid-cols-2">
            <FilePane label="左侧" state={paneA} setState={setPaneA} nodes={nodeItems} other={paneB} />
            <FilePane label="右侧" state={paneB} setState={setPaneB} nodes={nodeItems} other={paneA} />
          </div>
        )}
      </AsyncPanel>

      <SectionDivider label="多目标分发与断点偏移恢复" caption="Multi-Target Distribution & Offset Resume" />
      <div className="grid gap-5 xl:grid-cols-2 xl:items-start">
        <FileDistributionPanel key={`dist:${session.viewEpoch}`} nodes={nodeItems} />
        <AgentResumePanel key={session.viewEpoch} />
      </div>

      <TransferHistoryModal open={historyOpen} onClose={() => setHistoryOpen(false)} />
    </div>
  );
}
