package com.jolufasoft.capimux;
import org.junit.Test;
import static org.junit.Assert.*;

public class NavigationPolicyTest {
    @Test public void localPanelRequiresExactOrigin() {
        assertTrue(NavigationPolicy.isLocal("http://127.0.0.1:20131/dashboard"));
        for (String url : new String[]{"http://127.0.0.1:20130/", "https://127.0.0.1:20131/", "http://localhost:20131/", "http://127.0.0.1.evil.test:20131/", "http://user@127.0.0.1:20131/", "file:///data/data/", "javascript:alert(1)", "intent://test", "not a url"}) {
            assertFalse(url, NavigationPolicy.isLocal(url));
        }
    }
    @Test public void externalNavigationOnlyAllowsWebAddresses() {
        assertTrue(NavigationPolicy.isExternalWeb("https://example.com/oauth"));
        assertFalse(NavigationPolicy.isExternalWeb("http://127.0.0.1:20131/"));
        assertFalse(NavigationPolicy.isExternalWeb("https://user:secret@example.com/"));
        assertFalse(NavigationPolicy.isExternalWeb("content://settings"));
    }
}
