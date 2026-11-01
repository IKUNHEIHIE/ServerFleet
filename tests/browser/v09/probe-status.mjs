// Discovery of an unavailable backend is distinct from a successful workflow.
// Unexpected authentication, fixture or server failures must remain failures.
export function expectedUnavailable(response, expected) {
  const status = response?.status;
  if (Number.isInteger(status) && status >= 200 && status < 300) return false;
  const code = response?.body?.error?.code ?? response?.body?.code;
  if (expected.some(([allowedStatus, allowedCode]) => allowedStatus === status && allowedCode === code)) return true;
  throw new Error(`unexpected probe response: ${status} ${typeof code === "string" ? code : "missing_code"}`);
}
