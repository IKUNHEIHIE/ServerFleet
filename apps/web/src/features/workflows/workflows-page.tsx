import { PageHeader, Panel } from "@/components/ui/panel";
import { Notice } from "@/components/ui/feedback";

const REQUIRED = [
  "触发器、条件、执行步骤、通知、人工审批、等待与重试的可视化规则",
  "规则启停、版本冲突保护与操作审计",
  "人工审批决策与过期/撤权后的计划失效",
  "执行必须经统一任务系统、当前权限与 AI 资源/三级审批边界",
];

export function WorkflowsPage() {
  return (
    <div className="space-y-5">
      <PageHeader
        title="自动化工作流"
        description="产品要求已批准，但当前 OpenAPI 没有 workflows 管理端点。本页只说明契约缺口，不提供伪造的规则编辑器或执行成功。"
        badge={<span className="rounded-md border border-amber-600/40 bg-amber-50 px-2 py-1 text-xs text-amber-900">契约缺口</span>}
      />
      <Notice tone="warning" title="BLOCKED：无工作流接口">
        当前 `g2-merged-candidate.yaml` 没有 `/api/v1/workflows*` 操作；在没有服务端契约与授权/审批/审计接入前，
        本页不会创建本地规则、不发假通知、不模拟执行成功。精确最小请求已登记在
        `docs/development/sprints/v09-five-day/progress/gemini/SHARED-CHANGES.md` §2。
      </Notice>
      <Panel title="已批准但待接线的工作流能力">
        <ul className="list-disc space-y-2 px-8 py-5 text-sm leading-6">
          {REQUIRED.map(item => <li key={item}>{item}</li>)}
        </ul>
        <p className="border-t px-5 py-3 text-xs text-muted-foreground">
          现有可用替代：任务中心负责统一执行/审批/取消；AI 审批审计提供工具决策记录。它们不能冒充工作流规则引擎。
        </p>
      </Panel>
    </div>
  );
}
