import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { jsonResponse } from "@/test/http";
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  BarChart: ({ data }: { data: unknown }) => <output data-testid="telemetry">{JSON.stringify(data)}</output>,
  Bar: () => null, CartesianGrid: () => null, Tooltip: () => null, XAxis: () => null, YAxis: () => null,
}));
afterEach(() => { vi.unstubAllGlobals(); window.location.hash = ""; });
it("keeps missing and invalid telemetry distinct from measured zero and excludes offline readings", async () => {
  await renderAuthenticated("/overview", { "GET /api/v1/nodes": () => jsonResponse({ items: [
    { id: "missing", name: "Missing", online: true, cpuPercent: null, memoryPercent: 60 },
    { id: "invalid", name: "Invalid", online: true, cpuPercent: 101, memoryPercent: 40 },
    { id: "zero", name: "Zero", online: true, cpuPercent: 0, memoryPercent: 100 },
    { id: "offline", name: "Offline", online: false, cpuPercent: 80, memoryPercent: 80 },
  ] }) });
  await waitFor(() => expect(JSON.parse(screen.getByTestId("telemetry").textContent!)).toEqual([
    { name: "Missing", cpu: null, memory: 60 },
    { name: "Invalid", cpu: null, memory: 40 },
    { name: "Zero", cpu: 0, memory: 100 },
  ]));
});
