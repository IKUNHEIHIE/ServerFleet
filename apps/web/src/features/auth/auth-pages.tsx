import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Field, TextInput } from "@/components/ui/form";
import { Notice } from "@/components/ui/feedback";
import { describeApiError } from "@/lib/api/errors";
import type { ApiError } from "@/lib/api/errors";

export function AuthLayout({
  title,
  subtitle,
  children,
  footer,
}: {
  readonly title: string;
  readonly subtitle: ReactNode;
  readonly children: ReactNode;
  readonly footer?: ReactNode;
}) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="border-b bg-card">
        <div className="mx-auto flex w-full max-w-6xl items-center gap-3 px-5 py-4">
          <span aria-hidden="true" className="grid size-8 place-items-center rounded-md bg-primary text-sm font-bold text-primary-foreground">
            SF
          </span>
          <div>
            <p className="font-semibold leading-5">Server Fleet</p>
            <p className="text-xs text-muted-foreground">集中管理 · 安全授权</p>
          </div>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-6xl flex-1 items-center justify-center px-5 py-10">
        <section className="w-full max-w-md rounded-xl border bg-card p-6 shadow-sm" aria-labelledby="auth-title">
          <h1 id="auth-title" tabIndex={-1} className="text-xl font-semibold tracking-tight focus:outline-none">
            {title}
          </h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">{subtitle}</p>
          <div className="mt-6">{children}</div>
        </section>
      </main>
      {footer ? <footer className="border-t px-5 py-3 text-center text-xs text-muted-foreground">{footer}</footer> : null}
    </div>
  );
}

function useLoginSubmit(handler: () => Promise<{ ok: boolean; error: ApiError | null }>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    const result = await handler();
    setBusy(false);
    if (!result.ok) setError(result.error);
    return result;
  }
  return { busy, error, submit, setError };
}

export function LoginPage({ next }: { readonly next: string | null }) {
  const session = useSession();
  const alreadySignedIn = session.state.kind === "authenticated";
  const usernameId = useId();
  const passwordId = useId();
  const secondFactorId = useId();
  const usernameRef = useRef<HTMLInputElement | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [secondFactor, setSecondFactor] = useState("");
  const [notice] = useState<string | null>(
    session.state.kind === "anonymous" || session.state.kind === "authenticated" ? session.state.notice : null,
  );

  useEffect(() => {
    usernameRef.current?.focus();
  }, []);

  const { busy, error, submit } = useLoginSubmit(async () => {
    const result = await session.login(username, password, secondFactor.trim() || undefined);
    setPassword("");
    if (result.ok) {
      window.history.replaceState(null, "", `#${next && next !== "/login" ? next : "/overview"}`);
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    }
    return result;
  });

  return (
    <AuthLayout
      title={alreadySignedIn ? "重新登录" : "登录 Server Fleet"}
      subtitle={
        alreadySignedIn
          ? "重新登录会替换当前浏览器会话；用于恢复安全写入令牌或切换账号。"
          : "会话通过服务端 HttpOnly Cookie 建立。浏览器不读取、不保存会话令牌。"
      }
      footer={<>当前仅连接本机 Controller；不使用生产凭据或远程云资源。</>}
    >
      {notice ? <Notice tone="warning" title="会话提示" className="mb-4">{notice}</Notice> : null}
      {error ? (
        <Notice tone="error" title="登录未完成" className="mb-4">
          {describeApiError(error)}
        </Notice>
      ) : null}
      <form onSubmit={submit} noValidate className="space-y-4">
        <div>
          <label htmlFor={usernameId} className="block text-sm font-medium">
            用户名
          </label>
          <TextInput
            ref={usernameRef}
            id={usernameId}
            name="username"
            autoComplete="username"
            value={username}
            onChange={event => setUsername(event.target.value)}
            required
            disabled={busy}
          />
        </div>
        <div>
          <label htmlFor={passwordId} className="block text-sm font-medium">
            密码
          </label>
          <TextInput
            id={passwordId}
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={event => setPassword(event.target.value)}
            required
            disabled={busy}
          />
        </div>
        <div>
          <label htmlFor={secondFactorId} className="block text-sm font-medium">
            二次验证码 <span className="font-normal text-muted-foreground">（如已启用）</span>
          </label>
          <TextInput
            id={secondFactorId}
            name="secondFactorCode"
            autoComplete="one-time-code"
            value={secondFactor}
            onChange={event => setSecondFactor(event.target.value)}
            disabled={busy}
          />
        </div>
        <Button type="submit" variant="primary" className="w-full" loading={busy}>
          {alreadySignedIn ? "替换当前会话并登录" : "登录"}
        </Button>
      </form>
      {alreadySignedIn ? (
        <p className="mt-4 text-center text-sm">
          <a
            className="text-primary underline underline-offset-2"
            href="#/overview"
            onClick={event => {
              event.preventDefault();
              window.history.replaceState(null, "", "#/overview");
              window.dispatchEvent(new HashChangeEvent("hashchange"));
            }}
          >
            返回管理台
          </a>
        </p>
      ) : null}
    </AuthLayout>
  );
}

export function BootstrapPage({ available }: { readonly available: boolean }) {
  const session = useSession();
  const [token, setToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const { busy, error, submit } = useLoginSubmit(async () => {
    if (password !== confirm) {
      setLocalError("两次输入的密码不一致。");
      return { ok: false, error: null };
    }
    setLocalError(null);
    const result = await session.bootstrapSetup(token, username, password);
    if (result.ok) {
      setDone(true);
      window.history.replaceState(null, "", "#/overview");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    }
    return result;
  });
  const errorText = localError ?? (error ? describeApiError(error) : null);

  return (
    <AuthLayout
      title="初始化首位管理员"
      subtitle={
        available
          ? "使用本机安装时生成的一次性 setup token 创建管理员。该令牌只在此表单短暂输入，不写入浏览器存储。"
          : "该 Controller 已完成初始化，不能再次创建首位管理员。"
      }
      footer={<>初始化只在本机控制台令牌有效期内开放；不会自动创建远程账号。</>}
    >
      {!available ? (
        <Notice tone="info" title="已完成初始化">
          请使用已有管理员账号登录。
          <p className="mt-3">
            <a className="text-primary underline underline-offset-2" href="#/login">
              前往登录
            </a>
          </p>
        </Notice>
      ) : null}
      {done ? (
        <Notice tone="success" title="初始化成功">
          管理员会话已建立，正在进入管理台。
        </Notice>
      ) : null}
      {available && !done ? (
        <form onSubmit={submit} noValidate className="space-y-4">
          {errorText ? (
            <Notice tone="error" title="初始化未完成">
              {errorText}
            </Notice>
          ) : null}
          <Field label="本机 setup token">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="off"
                value={token}
                onChange={event => setToken(event.target.value)}
                required
                disabled={busy}
              />
            )}
          </Field>
          <Field label="管理员用户名">
            {ids => (
              <TextInput
                id={ids.inputId}
                autoComplete="username"
                value={username}
                onChange={event => setUsername(event.target.value)}
                required
                disabled={busy}
              />
            )}
          </Field>
          <Field label="密码" hint="至少使用服务端策略要求的最小长度；密码不会写入浏览器存储。">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={event => setPassword(event.target.value)}
                required
                disabled={busy}
              />
            )}
          </Field>
          <Field label="确认密码">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="new-password"
                value={confirm}
                onChange={event => setConfirm(event.target.value)}
                required
                disabled={busy}
              />
            )}
          </Field>
          <Button type="submit" variant="primary" className="w-full" loading={busy}>
            创建管理员并进入
          </Button>
        </form>
      ) : null}
    </AuthLayout>
  );
}
