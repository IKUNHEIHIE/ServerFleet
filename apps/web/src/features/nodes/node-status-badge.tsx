import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { NodeListItemView } from "./types";
import { nodeStatusMeta } from "./view-model";

/**
 * 节点状态徽标。
 * 颜色只是辅助，文字标签才是主要信息；列表与详情共用，避免两处文案漂移。
 */
export function NodeStatusBadge({ node }: { readonly node: NodeListItemView }) {
  const meta = nodeStatusMeta[node.status];
  const isOnline = node.status === "online";
  return (
    <Badge variant="outline" className={cn("gap-1.5 whitespace-nowrap transition-all duration-200", meta.badgeClassName)}>
      <span aria-hidden="true" className="relative inline-flex size-2 items-center justify-center">
        {isOnline ? (
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-75" />
        ) : null}
        <span className={cn("relative size-1.5 rounded-full", meta.dotClassName)} />
      </span>
      {meta.label}
    </Badge>
  );
}
