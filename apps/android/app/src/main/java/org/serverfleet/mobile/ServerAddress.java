package org.serverfleet.mobile;

import java.net.URI;
import java.net.URISyntaxException;

/** A Server Fleet installation occupies an origin, including a custom port. */
final class ServerAddress {
    static String normalize(String input) {
        String value = input.trim();
        if (!value.contains("://")) value = "https://" + value;
        try {
            URI uri = new URI(value);
            if (!"https".equalsIgnoreCase(uri.getScheme()) || uri.getHost() == null ||
                    uri.getUserInfo() != null || uri.getPort() == 0 || uri.getPort() > 65535) {
                throw new IllegalArgumentException("请填写有效的 HTTPS 网址，例如 https://agent.wentian.org:8443");
            }
            return new URI("https", null, uri.getHost().toLowerCase(java.util.Locale.ROOT),
                    uri.getPort() == 443 ? -1 : uri.getPort(), null, null, null).toASCIIString();
        } catch (URISyntaxException e) {
            throw new IllegalArgumentException("网址格式不正确，请检查域名和端口。");
        }
    }
    static boolean sameOrigin(String origin, String target) {
        try { return normalize(target).equals(origin); }
        catch (IllegalArgumentException e) { return false; }
    }
    private ServerAddress() {}
}
