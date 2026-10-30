import { Component, type ErrorInfo, type ReactNode } from "react";

interface State {
  readonly error: Error | null;
}

export class ErrorBoundary extends Component<{ readonly children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // 不打印堆栈到 UI；控制台只保留错误类型，避免泄露敏感视图数据。
    console.error("UI boundary:", error.name, info.componentStack?.split("\n")[1] ?? "");
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="mx-auto flex min-h-screen max-w-xl flex-col justify-center px-6">
        <h1 className="text-xl font-semibold">界面遇到未处理的错误</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          已停止渲染当前视图，避免继续展示可能过期的数据。可重新加载页面；若问题持续，请记录当前 URL 与操作步骤。
        </p>
        <button
          type="button"
          className="mt-5 h-9 w-fit rounded-md border bg-card px-4 text-sm font-medium"
          onClick={() => window.location.reload()}
        >
          重新加载
        </button>
      </div>
    );
  }
}
