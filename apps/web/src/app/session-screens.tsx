import { useSession } from "@/app/session";
import { AuthLayout } from "@/features/auth/auth-pages";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { describeApiError } from "@/lib/api/errors";

export function SessionChecking() {
  return (
    <div className="grid min-h-screen place-items-center px-6">
      <div role="status" aria-live="polite" className="flex flex-col items-center gap-3">
        <span aria-hidden="true" className="size-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        <p className="text-sm text-muted-foreground">正在验证服务端会话…</p>
      </div>
    </div>
  );
}

export function SessionUnavailable() {
  const session = useSession();
  if (session.state.kind !== "unavailable") return null;
  return (
    <AuthLayout
      title="无法验证管理会话"
      subtitle="Controller 管理 API 未返回可用响应。当前不会自动跳转登录页，也不会用演示数据替代。"
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
        如果错误码为 <code>404</code> 或提示“非 JSON 响应”，通常表示该 Controller 仍是工程骨架，尚未提供
        <code>/api/v1/auth/*</code> 管理接口。此页面不会伪造登录成功。
      </p>
    </AuthLayout>
  );
}
