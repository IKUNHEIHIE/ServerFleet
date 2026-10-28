import { Badge } from "@/components/ui/badge";

export function App() {
  return (
    <main className="mx-auto flex min-h-screen max-w-3xl flex-col justify-center gap-6 px-6 py-16">
      <div><Badge>M0 · P1</Badge></div>
      <h1 className="text-4xl font-semibold tracking-tight">Server Fleet</h1>
      <p className="text-lg text-muted-foreground">集中管理，从清晰的边界开始。</p>
      <section aria-labelledby="status-title" className="rounded-xl border bg-card p-6">
        <h2 id="status-title" className="font-semibold">工程基础已就绪</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          当前为开发骨架。节点接入、任务执行与插件功能尚未开放。
        </p>
      </section>
    </main>
  );
}
