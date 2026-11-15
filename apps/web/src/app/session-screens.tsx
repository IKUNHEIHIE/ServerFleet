import { MovingBorder, Spotlight } from "@/components/aceternity";
import { useSession } from "@/app/session";
import { AuthLayout } from "@/features/auth/auth-pages";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { describeApiError } from "@/lib/api/errors";

export function SessionChecking() {
  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden bg-background px-6 neurox-grid-bg">
      <Spotlight className="-top-32 left-0 md:left-40" />
      <MovingBorder containerClassName="w-full max-w-sm shadow-[var(--shadow-card-hover)]" className="px-6 py-8">
        <div role="status" aria-live="polite" className="flex flex-col items-center gap-3 text-center">
          <span
            aria-hidden="true"
            className="size-7 animate-spin rounded-full border-2 border-primary border-t-transparent"
          />
          <p className="text-sm font-medium text-muted-foreground">正在验证服务端会话…</p>
        </div>
      </MovingBorder>
    </div>
  );
}

export function SessionUnavailable() {
  const session = useSession();
  if (session.state.kind !== "unavailable") return null;
  return (
    <AuthLayout
      title="无法验证管理会话"
      subtitle="无法连接管理服务。请重试或重新加载页面。"
      footer={<>请确认 Controller 已启动、反向代理与同源 Cookie 配置正确。</>}
    >
      <Notice tone="error" title="连接失败">
        {describeApiError(session.state.error)}
      </Notice>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button type="button" variant="primary" onClick={session.retryInitialise}>
          重新检查
        </Button>
        <Button type="button" onClick={() => window.location.reload()}>
          重新加载页面
        </Button>
      </div>
      <p className="mt-4 text-xs leading-5 text-muted-foreground">
        无法连接管理服务。请确认服务已正常启动，或重新加载页面。
      </p>
    </AuthLayout>
  );
}
