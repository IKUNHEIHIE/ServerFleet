import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NodeManagementAccess } from "./node-management-access";
import { usersApi } from "@/lib/api/endpoints";
const state = vi.hoisted(() => ({ admin: true, locked: false, revalidate: vi.fn() }));
vi.mock("@/app/session", () => ({ useSession: () => ({ identity: { userId: "owner", isBootstrapAdmin: false, roles: state.admin ? ["administrator"] : [] }, can: () => state.admin, writesLocked: state.locked, revalidate: state.revalidate }) }));
vi.mock("@/lib/api/endpoints", () => ({ usersApi: { createGrant: vi.fn() } }));
afterEach(() => { vi.resetAllMocks(); state.admin = true; state.locked = false; });
it("requires explicit rights and grants only the selected node", async () => {
  vi.mocked(usersApi.createGrant).mockResolvedValue({ data: {} as never, status: 201 });
  const onGranted = vi.fn();
  render(<NodeManagementAccess nodeId="node-a" onGranted={onGranted} />);
  expect(screen.getByRole("button", { name: "授权当前账户管理此节点" })).toBeDisabled();
  expect(usersApi.createGrant).not.toHaveBeenCalled();
  fireEvent.click(screen.getByLabelText("编辑节点资料"));
  fireEvent.click(screen.getByRole("button", { name: "授权当前账户管理此节点" }));
  await waitFor(() => expect(usersApi.createGrant).toHaveBeenCalledWith({ principalId: "owner", resourceType: "node", resourceId: "node-a", permissions: ["nodes.read", "nodes.write"] }));
  expect(await screen.findByText("授权已保存")).toBeVisible();
  expect(onGranted).toHaveBeenCalledOnce();
});
it("does not expose grant creation to ordinary users", () => {
  state.admin = false;
  const { container } = render(<NodeManagementAccess nodeId="node-a" />);
  expect(container).toBeEmptyDOMElement();
  expect(usersApi.createGrant).not.toHaveBeenCalled();
});
