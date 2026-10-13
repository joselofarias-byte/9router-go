package com.jolufasoft.capimux;

import java.net.URI;
import java.net.URISyntaxException;

final class NavigationPolicy {
    static final int PORT = 20131;
    static final String ORIGIN = "http://127.0.0.1:" + PORT;

    static boolean isLocal(String value) {
        try {
            URI uri = new URI(value);
            return "http".equals(uri.getScheme()) && "127.0.0.1".equals(uri.getHost())
                && uri.getPort() == PORT && uri.getRawUserInfo() == null;
        } catch (URISyntaxException | NullPointerException e) {
            return false;
        }
    }

    static boolean isExternalWeb(String value) {
        try {
            URI uri = new URI(value);
            return ("https".equals(uri.getScheme()) || "http".equals(uri.getScheme()))
                && uri.getHost() != null && uri.getRawUserInfo() == null && !isLocal(value);
        } catch (URISyntaxException | NullPointerException e) {
            return false;
        }
    }
}
