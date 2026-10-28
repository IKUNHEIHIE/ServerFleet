import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// An unexpected network call must fail, never silently reach a real Controller.
vi.stubGlobal("fetch", () => Promise.reject(new Error("Network disabled in component tests")));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
