package org.serverfleet.mobile;

import org.junit.Test;
import static org.junit.Assert.*;

public class ServerAddressTest {
    @Test public void acceptsConsoleLinksAndCustomPorts() {
        assertEquals("https://agent.wentian.org:8443", ServerAddress.normalize(" https://agent.wentian.org:8443/#/ai "));
        assertEquals("https://example.com", ServerAddress.normalize("EXAMPLE.com:443"));
        assertEquals("https://[::1]:8443", ServerAddress.normalize("https://[::1]:8443/#/app"));
    }
    @Test public void originsIncludeSchemeHostAndPort() {
        assertTrue(ServerAddress.sameOrigin("https://example.com", "https://example.com:443/#/app/nodes"));
        assertFalse(ServerAddress.sameOrigin("https://example.com", "https://example.com:8443"));
        assertFalse(ServerAddress.sameOrigin("https://example.com", "http://example.com"));
        assertFalse(ServerAddress.sameOrigin("https://example.com", "https://example.com.evil.test"));
    }
    @Test public void invalidEntriesHaveAnActionableError() {
        for (String value : new String[]{"", "not a domain", "https://", "https://example.com:0", "https://example.com:65536", "https://user:password@example.com"}) {
            try { ServerAddress.normalize(value); fail(value); }
            catch (IllegalArgumentException expected) { assertFalse(expected.getMessage().isEmpty()); }
        }
    }
}
