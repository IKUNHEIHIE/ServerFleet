import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// 整套并行运行时（36+ 文件同时抢占 CPU），页面级用例在负载峰值会超过 vitest 默认 5s
// 单用例超时并随机抖动；这里是测试基础设施超时，不代表生产性能预算。
vi.setConfig({ testTimeout: 30_000, hookTimeout: 30_000 });

// An unexpected network call must fail, never silently reach a real Controller.
vi.stubGlobal("fetch", () => Promise.reject(new Error("Network disabled in component tests")));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
