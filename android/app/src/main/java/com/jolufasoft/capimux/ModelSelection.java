package com.jolufasoft.capimux;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;

/** Selection is independent of the visible search results. */
final class ModelSelection {
    static final int LIMIT = 8;
    private final Set<String> selected = new LinkedHashSet<>();
    ModelSelection(Collection<String> saved) { selected.addAll(saved); }
    boolean contains(String id) { return selected.contains(id); }
    int size() { return selected.size(); }
    List<String> values() { return new ArrayList<>(selected); }
    boolean toggle(String id, boolean checked) {
        if (!checked) { selected.remove(id); return true; }
        if (selected.contains(id)) return true;
        if (selected.size() >= LIMIT) return false;
        selected.add(id);
        return true;
    }
    static boolean matches(String id, String provider, String query) {
        String term = query.trim().toLowerCase(Locale.ROOT);
        return (id + " " + provider).toLowerCase(Locale.ROOT).contains(term);
    }
}
