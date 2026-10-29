import { PageHeader, Panel } from "@/components/ui/panel";

export function NotFoundPage({ path }: { readonly path: string }) {
  return (
    <div className="space-y-5">
      <PageHeader title="页面不存在" description="当前地址未匹配任何已登记的管理台路由。" />
      <Panel title="找不到页面">
        <div className="px-6 py-8">
          <p className="text-sm text-muted-foreground">
            请求路径：<code>{path}</code>
          </p>
          <p className="mt-2 text-sm">
            <a className="text-primary underline underline-offset-2" href="#/overview">
              返回概览仪表盘
            </a>
          </p>
        </div>
      </Panel>
    </div>
  );
}
