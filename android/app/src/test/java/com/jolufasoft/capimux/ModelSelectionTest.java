package com.jolufasoft.capimux;

import org.junit.Test;
import java.util.List;
import static org.junit.Assert.*;

public final class ModelSelectionTest {
    @Test public void searchingAcrossProvidersKeepsSelectedModels() {
        ModelSelection selection = new ModelSelection(List.of("provider-one/model-a"));
        assertFalse(ModelSelection.matches("provider-one/model-a", "Provider One", "TWO"));
        assertTrue(ModelSelection.matches("provider-two/model-b", "Provider Two", "TWO"));
        assertTrue(selection.toggle("provider-two/model-b", true));
        assertEquals(List.of("provider-one/model-a", "provider-two/model-b"), selection.values());
        assertTrue(selection.contains("provider-one/model-a"));
    }
    @Test public void limitRejectsNinthModelButAllowsRemovalAndReplacement() {
        ModelSelection selection = new ModelSelection(List.of());
        for (int i = 0; i < 8; i++) assertTrue(selection.toggle("p/model-" + i, true));
        assertFalse(selection.toggle("p/ninth", true));
        assertTrue(selection.toggle("p/model-0", false));
        assertTrue(selection.toggle("p/ninth", true));
        assertEquals(8, selection.size());
        assertFalse(selection.contains("p/model-0"));
    }
    @Test public void savedSelectionPreservesOrderAndDeduplicates() {
        ModelSelection selection = new ModelSelection(List.of("b/model", "a/model", "b/model"));
        assertEquals(List.of("b/model", "a/model"), selection.values());
    }
}
