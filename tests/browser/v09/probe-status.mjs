// Discovery of an unavailable backend is distinct from a successful workflow.
// Unexpected authentication, fixture or server failures must remain failures.
export function expectedUnavailable(response, expected) {
  const status = response?.status;
  if (Number.isInteger(status) && status >= 200 && status < 300) return false;
  const code = response?.body?.error?.code ?? response?.body?.code;
  if (expected.some(([allowedStatus, allowedCode]) => allowedStatus === status && allowedCode === code)) return true;
  throw new Error(`unexpected probe response: ${status} ${typeof code === "string" ? code : "missing_code"}`);
}

// The real recovery DTO uses boolean `passed`, not a `status` string. A
// malformed/empty/error response cannot become a missing-environment verdict.
export function recoveryPreviewReady(response) {
  if (response?.status !== 200 || !Array.isArray(response.body?.checks) || response.body.checks.length === 0 ||
      response.body.checks.some(check => typeof check.name !== "string" || typeof check.passed !== "boolean")) {
    throw new Error("invalid recovery preview response");
  }
  return response.body.checks.every(check => check.passed);
}
