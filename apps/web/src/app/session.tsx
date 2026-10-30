import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { authApi } from "@/lib/api/endpoints";
import { setCsrfProvider, onUnauthorized } from "@/lib/api/client";
import { ApiError, describeApiError, isApiError } from "@/lib/api/errors";
import type { BootstrapState, Identity, LoginResult } from "@/lib/api/dto";

export type SessionState =
  | { readonly kind: "checking" }
  | { readonly kind: "anonymous"; readonly notice: string | null }
  | { readonly kind: "bootstrap"; readonly bootstrap: BootstrapState; readonly notice: string | null }
  | { readonly kind: "unavailable"; readonly error: ApiError }
  | {
      readonly kind: "authenticated";
      readonly identity: Identity;
      readonly csrfToken: string | null;
      readonly sessionExpiresAt: string | null;
      readonly warning: string | null;
      readonly notice: string | null;
    };

export interface SessionActionResult {
  readonly ok: boolean;
  readonly error: ApiError | null;
}

interface BroadcastLogin {
  readonly type: "login";
  readonly identity: Identity;
  readonly csrfToken: string | null;
  readonly sessionExpiresAt: string | null;
}
interface BroadcastLogout {
  readonly type: "logout" | "expired";
}
type BroadcastMessage = BroadcastLogin | BroadcastLogout;

const CHANNEL = "serverfleet.session";

export interface SessionContextValue {
  readonly state: SessionState;
  readonly identity: Identity | null;
  readonly epoch: number;
  readonly resumeEpoch: number;
  /** 资源 key 的一部分：身份变化或页面恢复时让旧 DOM/迟到响应立即失效。 */
  readonly viewEpoch: string;
  /** 插件安装/启停后递增，让侧边栏的插件入口立刻重新读取服务端状态。 */
  readonly pluginEpoch: number;
  readonly refreshPlugins: () => void;
  readonly writesLocked: boolean;
  readonly can: (permission: string) => boolean;
  readonly login: (username: string, password: string, secondFactorCode?: string) => Promise<SessionActionResult>;
  readonly bootstrapSetup: (setupToken: string, username: string, password: string) => Promise<SessionActionResult>;
  readonly logout: () => Promise<SessionActionResult>;
  readonly retryInitialise: () => void;
  readonly revalidate: () => void;
  readonly clearNotice: () => void;
}

const SessionContext = createContext<SessionContextValue | null>(null);

function validIdentity(value: unknown): value is Identity {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.userId === "string" &&
    record.userId.length > 0 &&
    typeof record.displayName === "string" &&
    Array.isArray(record.roles) &&
    record.roles.every(role => typeof role === "string") &&
    (record.permissions === undefined ||
      (Array.isArray(record.permissions) && record.permissions.every(permission => typeof permission === "string")))
  );
}

function parseLoginResult(result: LoginResult | null): LoginResult | null {
  if (!result || !validIdentity(result.identity)) return null;
  const csrf = typeof result.csrfToken === "string" && result.csrfToken.length > 0 ? result.csrfToken : null;
  const expires = typeof result.sessionExpiresAt === "string" && !Number.isNaN(Date.parse(result.sessionExpiresAt))
    ? result.sessionExpiresAt
    : null;
  if (!expires) return null;
  return { ...result, csrfToken: csrf ?? undefined, sessionExpiresAt: expires };
}

function failure(error: unknown): SessionActionResult {
  return {
    ok: false,
    error: isApiError(error) ? error : new ApiError({ kind: "unknown", status: 0, cause: error }),
  };
}

export function SessionProvider({ children }: { readonly children: ReactNode }) {
  const csrfRef = useRef<string | null>(null);
  const [state, setState] = useState<SessionState>({ kind: "checking" });
  const stateRef = useRef<SessionState>(state);
  const [epoch, setEpoch] = useState(0);
  const [resumeEpoch, setResumeEpoch] = useState(0);
  const [pluginEpoch, setPluginEpoch] = useState(0);
  const [initAttempt, setInitAttempt] = useState(0);
  const revalidating = useRef(false);
  const lastRevalidateAt = useRef(0);
  const channelRef = useRef<BroadcastChannel | null>(null);

  const commit = useCallback((next: SessionState) => {
    csrfRef.current = next.kind === "authenticated" ? next.csrfToken : null;
    stateRef.current = next;
    setState(next);
  }, []);

  const bumpEpoch = useCallback(() => setEpoch(value => value + 1), []);
  const refreshPlugins = useCallback(() => setPluginEpoch(value => value + 1), []);

  useEffect(() => {
    setCsrfProvider(() => csrfRef.current);
    return () => setCsrfProvider(() => null);
  }, []);

  const finishLogin = useCallback(
    (raw: LoginResult, broadcast: boolean) => {
      const result = parseLoginResult(raw);
      if (!result) {
        return { ok: false, error: new ApiError({ kind: "invalid", status: 200 }) } satisfies SessionActionResult;
      }
      commit({
        kind: "authenticated",
        identity: result.identity,
        csrfToken: result.csrfToken ?? null,
        sessionExpiresAt: result.sessionExpiresAt,
        warning: null,
        notice: null,
      });
      bumpEpoch();
      if (broadcast && channelRef.current) {
        const message: BroadcastLogin = {
          type: "login",
          identity: result.identity,
          csrfToken: result.csrfToken ?? null,
          sessionExpiresAt: result.sessionExpiresAt,
        };
        channelRef.current.postMessage(message);
      }
      return { ok: true, error: null } satisfies SessionActionResult;
    },
    [bumpEpoch, commit],
  );

  // 初始化：先看 bootstrap 状态，再验证 Cookie 会话。
  useEffect(() => {
    let cancelled = false;
    commit({ kind: "checking" });
    void (async () => {
      try {
        const bootstrap = await authApi.bootstrapStatus();
        if (cancelled) return;
        if (bootstrap.data.bootstrapAvailable && !bootstrap.data.initialized) {
          commit({ kind: "bootstrap", bootstrap: bootstrap.data, notice: null });
          return;
        }
        try {
          const me = await authApi.me();
          if (cancelled) return;
          if (!validIdentity(me.data)) {
            commit({ kind: "unavailable", error: new ApiError({ kind: "invalid", status: me.status }) });
            return;
          }
          commit({
            kind: "authenticated",
            identity: me.data,
            // 刷新后 CSRF 需受保护地补取；当前 Identity 契约没有该字段，写入先锁定。
            csrfToken: null,
            sessionExpiresAt: null,
            warning: null,
            notice: null,
          });
          bumpEpoch();
        } catch (error) {
          if (cancelled) return;
          const apiError = isApiError(error) ? error : new ApiError({ kind: "unknown", status: 0, cause: error });
          if (apiError.kind === "auth") {
            commit({ kind: "anonymous", notice: null });
            return;
          }
          commit({ kind: "unavailable", error: apiError });
        }
      } catch (error) {
        if (cancelled) return;
        const apiError = isApiError(error) ? error : new ApiError({ kind: "unknown", status: 0, cause: error });
        commit({ kind: "unavailable", error: apiError });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [initAttempt, commit, bumpEpoch]);

  const expire = useCallback(
    (notice: string, broadcast: boolean) => {
      if (stateRef.current.kind !== "authenticated") return;
      commit({ kind: "anonymous", notice });
      bumpEpoch();
      if (broadcast && channelRef.current) channelRef.current.postMessage({ type: "expired" });
    },
    [bumpEpoch, commit],
  );

  useEffect(
    () =>
      onUnauthorized(() => {
        expire("登录会话已失效或已被撤销，请重新登录。", true);
      }),
    [expire],
  );

  const revalidate = useCallback(() => {
    if (stateRef.current.kind !== "authenticated" || revalidating.current) return;
    const now = Date.now();
    if (now - lastRevalidateAt.current < 1_500) return;
    lastRevalidateAt.current = now;
    revalidating.current = true;
    void (async () => {
      try {
        const me = await authApi.me();
        const current = stateRef.current;
        if (current.kind !== "authenticated") return;
        if (!validIdentity(me.data)) {
          commit({ ...current, warning: "身份刷新返回了无法识别的数据，已保留当前会话。" });
          return;
        }
        const changed = JSON.stringify(current.identity) !== JSON.stringify(me.data);
        commit({ ...current, identity: me.data, warning: null });
        if (changed) bumpEpoch();
      } catch (error) {
        const apiError = isApiError(error) ? error : new ApiError({ kind: "unknown", status: 0, cause: error });
        if (apiError.kind === "auth") return; // 全局监听器已切到登录页。
        const current = stateRef.current;
        if (current.kind !== "authenticated") return;
        commit({ ...current, warning: `身份刷新失败：${describeApiError(apiError)} 当前会话已保留。` });
      } finally {
        revalidating.current = false;
      }
    })();
  }, [bumpEpoch, commit]);

  // 跨标签同步：只通过 BroadcastChannel 在内存间传递，不写 localStorage/Cookie 明文。
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const channel = new BroadcastChannel(CHANNEL);
    channelRef.current = channel;
    channel.onmessage = (event: MessageEvent<unknown>) => {
      const data = event.data;
      if (typeof data !== "object" || data === null) return;
      const message = data as BroadcastMessage;
      if (message.type === "login") {
        if (!validIdentity(message.identity)) return;
        commit({
          kind: "authenticated",
          identity: message.identity,
          csrfToken: typeof message.csrfToken === "string" && message.csrfToken.length > 0 ? message.csrfToken : null,
          sessionExpiresAt:
            typeof message.sessionExpiresAt === "string" && !Number.isNaN(Date.parse(message.sessionExpiresAt))
              ? message.sessionExpiresAt
              : null,
          warning: null,
          notice: "已在其他标签页登录，当前标签页会话已同步。",
        });
        bumpEpoch();
        return;
      }
      if (message.type === "logout" || message.type === "expired") {
        if (stateRef.current.kind !== "authenticated") return;
        commit({
          kind: "anonymous",
          notice: message.type === "logout" ? "已在其他标签页退出登录。" : "其他标签页检测到会话失效，请重新登录。",
        });
        bumpEpoch();
      }
    };
    return () => {
      channel.onmessage = null;
      channel.close();
      channelRef.current = null;
    };
  }, [bumpEpoch, commit]);

  // BFCache / 页面重新可见：刷新身份并让当前页面丢弃旧数据、重新请求。
  useEffect(() => {
    if (typeof document === "undefined") return;
    let hidden = document.visibilityState === "hidden";
    const resume = () => {
      setResumeEpoch(value => value + 1);
      revalidate();
    };
    const onVisibility = () => {
      const nowHidden = document.visibilityState === "hidden";
      if (hidden && !nowHidden) resume();
      hidden = nowHidden;
    };
    const onPageShow = (event: PageTransitionEvent) => {
      if (event.persisted) resume();
    };
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pageshow", onPageShow);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pageshow", onPageShow);
    };
  }, [revalidate]);

  // 会话到期后主动复核，而不是本地直接改权限；401 由服务器裁决。
  useEffect(() => {
    if (state.kind !== "authenticated" || !state.sessionExpiresAt) return;
    const expires = Date.parse(state.sessionExpiresAt);
    if (Number.isNaN(expires)) return;
    const delay = Math.max(0, expires - Date.now()) + 500;
    const timer = setTimeout(() => revalidate(), Math.min(delay, 2_147_000_000));
    return () => clearTimeout(timer);
  }, [state, revalidate]);

  const login = useCallback(
    async (username: string, password: string, secondFactorCode?: string): Promise<SessionActionResult> => {
      try {
        const response = await authApi.login({
          username,
          password,
          ...(secondFactorCode ? { secondFactorCode } : {}),
        });
        return finishLogin(response.data, true);
      } catch (error) {
        return failure(error);
      }
    },
    [finishLogin],
  );

  const bootstrapSetup = useCallback(
    async (setupToken: string, username: string, password: string): Promise<SessionActionResult> => {
      try {
        const response = await authApi.bootstrap({ setupToken, username, password });
        return finishLogin(response.data, true);
      } catch (error) {
        return failure(error);
      }
    },
    [finishLogin],
  );

  const logout = useCallback(async (): Promise<SessionActionResult> => {
    const current = stateRef.current;
    if (current.kind !== "authenticated") return { ok: true, error: null };
    let serverError: ApiError | null = null;
    if (current.csrfToken) {
      try {
        await authApi.logout();
      } catch (error) {
        const apiError = isApiError(error) ? error : new ApiError({ kind: "unknown", status: 0, cause: error });
        if (apiError.kind !== "auth") serverError = apiError;
      }
    } else {
      serverError = new ApiError({ kind: "csrf-missing", status: 0 });
    }
    const notice = serverError
      ? `本地会话已清除，但未能向 Controller 确认撤销：${describeApiError(serverError)}`
      : null;
    commit({ kind: "anonymous", notice });
    bumpEpoch();
    if (channelRef.current) channelRef.current.postMessage({ type: "logout" });
    return { ok: serverError === null, error: serverError };
  }, [bumpEpoch, commit]);

  const identity = state.kind === "authenticated" ? state.identity : null;
  const can = useCallback(
    (permission: string): boolean => {
      if (!identity) return false;
      if (identity.isBootstrapAdmin === true) return true;
      return identity.permissions?.includes(permission) ?? false;
    },
    [identity],
  );

  const value: SessionContextValue = {
    state,
    identity,
    epoch,
    resumeEpoch,
    viewEpoch: `${epoch}:${resumeEpoch}`,
    pluginEpoch,
    refreshPlugins,
    writesLocked: state.kind === "authenticated" && !state.csrfToken,
    can,
    login,
    bootstrapSetup,
    logout,
    retryInitialise: () => setInitAttempt(value => value + 1),
    revalidate,
    clearNotice: () => {
      const current = stateRef.current;
      if (current.kind === "anonymous" || current.kind === "authenticated" || current.kind === "bootstrap") {
        commit({ ...current, notice: null });
      }
    },
  };

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const context = useContext(SessionContext);
  if (!context) throw new Error("useSession must be used inside SessionProvider");
  return context;
}
