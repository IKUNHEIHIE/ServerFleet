/** 浏览器验收桩的公共辅助；仅用于本地验收，不会被打包进 dist，也不被 apps/web/src 引用。 */
export function http(status, body) {
  return { __http: { status, body } };
}

export function unwrap(result) {
  if (result && typeof result === "object" && result.__http) return result.__http;
  return { status: 200, body: result };
}

export function page(items, extra = {}) {
  return { items, page: { nextCursor: null, limit: 100, hasMore: false }, ...extra };
}

export function errorBody(code, message) {
  return { code, message, requestId: `stub-${code.toLowerCase()}` };
}
