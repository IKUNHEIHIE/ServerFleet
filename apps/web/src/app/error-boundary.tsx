import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Panel } from "@/components/ui/panel";

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
        <Panel className="p-6">
          <h1 className="text-xl font-semibold text-foreground">界面遇到未处理的错误</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            已停止渲染当前视图，避免继续展示可能过期的数据。可重新加载页面；若问题持续，请记录当前 URL 与操作步骤。
          </p>
          <Button
            type="button"
            variant="primary"
            className="mt-5 w-fit"
            onClick={() => window.location.reload()}
          >
            重新加载
          </Button>
        </Panel>
      </div>
    );
  }
}
