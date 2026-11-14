import { Compass, ArrowLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { PageHeader, Panel } from "@/components/ui/panel";

export function NotFoundPage({ path }: { readonly path: string }) {
  return (
    <div className="space-y-5">
      <PageHeader
        title="页面不存在"
        description="当前地址未匹配任何已登记的管理台路由。"
        badge={<Badge tone="warning" dot>404</Badge>}
      />
      <Panel title="找不到页面">
        <div className="px-6 py-8">
          <div className="mb-3 inline-flex size-10 items-center justify-center rounded-xl border border-primary/20 bg-primary/10 text-primary">
            <Compass className="size-5" />
          </div>
          <p className="text-sm text-muted-foreground">
            请求路径：<code className="rounded bg-secondary px-1.5 py-0.5 font-mono text-xs">{path}</code>
          </p>
          <p className="mt-3 text-sm">
            <a className="inline-flex items-center gap-1.5 font-medium text-primary underline underline-offset-2" href="#/overview">
              <ArrowLeft className="size-3.5" />
              返回概览仪表盘
            </a>
          </p>
        </div>
      </Panel>
    </div>
  );
}
