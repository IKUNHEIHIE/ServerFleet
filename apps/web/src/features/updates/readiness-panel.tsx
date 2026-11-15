import { useState } from "react";
import { Activity, RefreshCw, Server } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Panel } from "@/components/ui/panel";
import type { HealthPayload } from "@/lib/api/dto";
import { systemApi, type ReadinessPayload } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";

interface ProbeSnapshot {
  readonly checkedAt: string;
  readonly readiness: ReadinessPayload | null;
  readonly readinessError: string | null;
  readonly health: HealthPayload | null;
  readonly healthError: string | null;
}

const SUBSYSTEM_LABELS: Readonly<Record<string, string>> = {
  database: "数据库 (SQLite WAL)",
  authority: "权威控制门禁",
  scheduler: "调度器",
  agentTransport: "Agent 传输通道",
  pki: "内部 PKI 签发",
};

function subsystemTone(status: string): "success" | "warning" | "danger" | "neutral" {
  switch (status) {
    case "ready":
      return "success";
    case "not_configured":
      return "neutral";
    case "unavailable":
      return "danger";
    default:
      return "warning";
  }
}

export function ReadinessPanel() {
  const [snapshot, setSnapshot] = useState<ProbeSnapshot | null>(null);

  const probe = useAction(async (): Promise<ProbeSnapshot> => {
    const [readinessRes, healthRes] = await Promise.allSettled([
      systemApi.readiness(),
      systemApi.health(),
    ]);
    return {
      checkedAt: new Date().toLocaleTimeString(),
      readiness: readinessRes.status === "fulfilled" ? readinessRes.value.data : null,
      readinessError: readinessRes.status === "rejected" ? describeApiError(readinessRes.reason) : null,
      health: healthRes.status === "fulfilled" ? healthRes.value.data : null,
      healthError: healthRes.status === "rejected" ? describeApiError(healthRes.reason) : null,
    };
  });

  async function handleProbe() {
    const result = await probe.run();
    if (result.ok && result.data) {
      setSnapshot(result.data);
    }
  }

  return (
    <Panel
      title="主控通用就绪与活性诊断"
      description="按需核验控制面启动与子系统就绪状态。"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="info" dot>
            <Activity aria-hidden="true" className="size-3" />
            Readyz &amp; Healthz
          </Badge>
          <Button size="sm" variant="primary" loading={probe.pending} onClick={() => void handleProbe()}>
            <RefreshCw aria-hidden="true" className="size-3.5" />
            探测主控就绪与活性
          </Button>
        </div>
      }
    >
      <div className="space-y-4 px-5 py-4">
        <Notice tone="info" title="诊断语义边界">
          通用 <code>/readyz</code> 检查启动及子系统就绪，<code>/healthz</code> 仅表示活性；均不能代替业务或节点健康，更新还有独立本地监督进程 nonce 就绪条件。
        </Notice>

        {snapshot ? (
          <div className="space-y-3.5">
            <div className="grid gap-3.5 sm:grid-cols-2">
              <div className="neurox-subcard space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-foreground">
                    <Server aria-hidden="true" className="size-3.5 text-primary" />
                    总体就绪状态
                  </span>
                  {snapshot.readiness ? (
                    <Badge tone={snapshot.readiness.status === "ready" ? "success" : "danger"} dot>
                      {snapshot.readiness.status}
                    </Badge>
                  ) : (
                    <Badge tone="danger" dot>探测失败</Badge>
                  )}
                </div>
                {snapshot.readinessError ? (
                  <p className="text-xs text-destructive">{snapshot.readinessError}</p>
                ) : (
                  <p className="text-xs text-muted-foreground">最近探测：{snapshot.checkedAt}</p>
                )}
              </div>

              <div className="neurox-subcard space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-foreground">
                    <Activity aria-hidden="true" className="size-3.5 text-sky-600" />
                    进程活性
                  </span>
                  {snapshot.health ? (
                    <Badge tone={snapshot.health.status === "ok" ? "success" : "warning"} dot>
                      {snapshot.health.status}
                    </Badge>
                  ) : (
                    <Badge tone="danger" dot>探测失败</Badge>
                  )}
                </div>
                {snapshot.healthError ? (
                  <p className="text-xs text-destructive">{snapshot.healthError}</p>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    Stage：{snapshot.health?.stage ?? "—"} · 最近探测：{snapshot.checkedAt}
                  </p>
                )}
              </div>
            </div>

            {snapshot.readiness ? (
              <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-5">
                {Object.entries(snapshot.readiness.checks).map(([key, value]) => (
                  <div key={key} className="neurox-subcard flex flex-col justify-between gap-1.5">
                    <span className="text-[11px] font-medium text-muted-foreground">
                      {SUBSYSTEM_LABELS[key] ?? key}
                    </span>
                    <div>
                      <Badge tone={subsystemTone(value)} dot>
                        {value}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            ) : null}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            点击右上角“探测主控就绪与活性”进行实时检查。
          </p>
        )}
      </div>
    </Panel>
  );
}
