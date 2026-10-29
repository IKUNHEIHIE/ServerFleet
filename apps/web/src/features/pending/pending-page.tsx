import { PageHeader, Panel } from "@/components/ui/panel";
import { Notice } from "@/components/ui/feedback";

export function PendingPage({ title, description }: { readonly title: string; readonly description?: string }) {
  return (
    <div className="space-y-5">
      <PageHeader title={title} description={description} badge={<span className="rounded-md border px-2 py-1 text-xs">实施中</span>} />
      <Panel title="本页尚未接入">
        <div className="px-6 py-6">
          <Notice tone="warning" title="不是已交付页面">
            导航与路由已保留，但该模块的真实接口适配、权限校验和浏览器验收尚未完成。此状态不会被描述为已完成；
            在完成前不提供任何伪造数据、模拟成功或可点击的假操作。
          </Notice>
          <p className="mt-4 text-sm text-muted-foreground">
            进度记录见 <code>docs/development/sprints/v09-five-day/progress/gemini/STATUS.md</code>。
          </p>
        </div>
      </Panel>
    </div>
  );
}

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
