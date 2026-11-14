import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { FileDistributionPanel } from "./file-distribution-panel";
import { fileDistributionsApi } from "@/lib/api/endpoints";

const access = vi.hoisted(() => ({ epoch: "1", writable: true }));
vi.mock("@/app/session", () => ({ useSession: () => ({ viewEpoch: access.epoch, writesLocked: !access.writable, can: () => access.writable }) }));
vi.mock("@/lib/api/endpoints", () => ({ fileDistributionsApi: { create: vi.fn(), uploadContent: vi.fn(), complete: vi.fn(), get: vi.fn(), cancel: vi.fn() } }));
afterEach(() => { vi.resetAllMocks(); vi.unstubAllGlobals(); access.epoch = "1"; access.writable = true; });

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
function selectFile(arrayBuffer: () => Promise<ArrayBuffer>) {
  fireEvent.click(screen.getByRole("button", { name: "打开多目标文件分发" }));
  fireEvent.change(screen.getByLabelText("选择本地分发文件"), { target: { files: [{ name: "sample.txt", arrayBuffer }] } });
  fireEvent.change(screen.getByLabelText(/分发目标节点 ID/), { target: { value: "n-1" } });
  fireEvent.submit(screen.getByLabelText("选择本地分发文件").closest("form")!);
}
it("does not create a distribution after its owner unmounts during file reading", async () => {
  const read = deferred<ArrayBuffer>();
  const digest = vi.fn(async () => new ArrayBuffer(32));
  vi.stubGlobal("crypto", { subtle: { digest } });
  const arrayBuffer = vi.fn(() => read.promise);
  const view = render(<FileDistributionPanel nodes={[]} />);
  selectFile(arrayBuffer);
  expect(arrayBuffer).toHaveBeenCalledOnce();
  view.unmount();
  await act(async () => { read.resolve(new ArrayBuffer(4)); await read.promise; });
  expect(digest).not.toHaveBeenCalled();
  expect(fileDistributionsApi.create).not.toHaveBeenCalled();
});
it("aborts an in-flight create and never uploads after the owner unmounts", async () => {
  vi.stubGlobal("crypto", { subtle: { digest: async () => new ArrayBuffer(32) } });
  const created = deferred<Awaited<ReturnType<typeof fileDistributionsApi.create>>>();
  vi.mocked(fileDistributionsApi.create).mockReturnValue(created.promise);
  const view = render(<FileDistributionPanel nodes={[]} />);
  selectFile(async () => new ArrayBuffer(4));
  await waitFor(() => expect(fileDistributionsApi.create).toHaveBeenCalledOnce());
  const signal = vi.mocked(fileDistributionsApi.create).mock.calls[0][1];
  expect(signal?.aborted).toBe(false);
  view.unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => { created.resolve({ data: { distributionId: "d-1", status: "uploading" }, status: 201 }); await created.promise; });
  expect(fileDistributionsApi.uploadContent).not.toHaveBeenCalled();
  expect(fileDistributionsApi.complete).not.toHaveBeenCalled();
});
it("uploads and completes with the same live operation signal", async () => {
  vi.stubGlobal("crypto", { subtle: { digest: async () => new ArrayBuffer(32) } });
  vi.mocked(fileDistributionsApi.create).mockResolvedValue({ data: { distributionId: "d-1", status: "uploading" }, status: 201 });
  vi.mocked(fileDistributionsApi.complete).mockResolvedValue({ data: { distributionId: "d-1", status: "queued", destination: "/tmp/sample.txt", bytesReceived: 4, targetNodes: [] }, status: 200 });
  render(<FileDistributionPanel nodes={[]} />);
  selectFile(async () => new ArrayBuffer(4));
  await waitFor(() => expect(fileDistributionsApi.complete).toHaveBeenCalledOnce());
  const signal = vi.mocked(fileDistributionsApi.create).mock.calls[0][1];
  expect(fileDistributionsApi.uploadContent).toHaveBeenCalledWith("d-1", expect.any(ArrayBuffer), 0, signal);
  expect(fileDistributionsApi.complete).toHaveBeenCalledWith("d-1", signal);
  expect(signal?.aborted).toBe(false);
});

it("does not create after unmounting while hashing", async () => {
  const hash = deferred<ArrayBuffer>();
  const digest = vi.fn(() => hash.promise);
  vi.stubGlobal("crypto", { subtle: { digest } });
  const view = render(<FileDistributionPanel nodes={[]} />);
  selectFile(async () => new ArrayBuffer(4));
  await waitFor(() => expect(digest).toHaveBeenCalledOnce());
  view.unmount();
  await act(async () => { hash.resolve(new ArrayBuffer(32)); await hash.promise; });
  expect(fileDistributionsApi.create).not.toHaveBeenCalled();
});
it("does not complete after unmounting while uploading", async () => {
  vi.stubGlobal("crypto", { subtle: { digest: async () => new ArrayBuffer(32) } });
  const upload = deferred<Awaited<ReturnType<typeof fileDistributionsApi.uploadContent>>>();
  vi.mocked(fileDistributionsApi.create).mockResolvedValue({ data: { distributionId: "d-1", status: "uploading" }, status: 201 });
  vi.mocked(fileDistributionsApi.uploadContent).mockReturnValue(upload.promise);
  const view = render(<FileDistributionPanel nodes={[]} />);
  selectFile(async () => new ArrayBuffer(4));
  await waitFor(() => expect(fileDistributionsApi.uploadContent).toHaveBeenCalledOnce());
  view.unmount();
  await act(async () => { upload.resolve({ data: { distributionId: "d-1", status: "uploading", destination: "/tmp/sample.txt", bytesReceived: 4, targetNodes: [] }, status: 200 }); await upload.promise; });
  expect(fileDistributionsApi.complete).not.toHaveBeenCalled();
});
it.each(["epoch", "permission"])("does not continue when %s changes during file reading", async change => {
  const read = deferred<ArrayBuffer>();
  const digest = vi.fn(async () => new ArrayBuffer(32));
  vi.stubGlobal("crypto", { subtle: { digest } });
  const view = render(<FileDistributionPanel nodes={[]} />);
  selectFile(() => read.promise);
  if (change === "epoch") access.epoch = "2";
  else access.writable = false;
  view.rerender(<FileDistributionPanel nodes={[]} />);
  await act(async () => { read.resolve(new ArrayBuffer(4)); await read.promise; });
  expect(digest).not.toHaveBeenCalled();
  expect(fileDistributionsApi.create).not.toHaveBeenCalled();
});
