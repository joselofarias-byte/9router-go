package com.jolufasoft.capimux;

import android.app.Activity;
import android.app.AlertDialog;
import android.text.Editable;
import android.text.TextWatcher;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;
import java.util.ArrayList;
import java.util.List;

final class ModelPicker {
    private final Activity activity;
    private final Runnable openProviders;
    private final Runnable openComparator;
    private final ModelStore store;
    private ModelSelection selection;
    private final List<LocalModels.Entry> catalog = new ArrayList<>();
    private LinearLayout list;
    private TextView summary;
    private EditText search;
    private CheckBox external;
    private AlertDialog dialog;
    private boolean loaded;

    ModelPicker(Activity activity, Runnable providers, Runnable comparator) {
        this.activity = activity;
        this.openProviders = providers;
        this.openComparator = comparator;
        this.store = new ModelStore(activity);
    }
    void show() {
        try {
            store.migrate(activity);
            selection = new ModelSelection(store.models());
            build(store.read().optBoolean("external_enabled", false));
            load();
        } catch (Exception error) {
            Toast.makeText(activity, "No se pudo abrir la selección de modelos.", Toast.LENGTH_LONG).show();
        }
    }
    private void build(boolean enabled) {
        LinearLayout form = new LinearLayout(activity);
        form.setOrientation(LinearLayout.VERTICAL);
        form.setPadding(24, 12, 24, 12);
        TextView help = new TextView(activity);
        help.setText("Elegí hasta 8 modelos. Se muestran los proveedores activos y los modelos que no requieren una clave.");
        form.addView(help);
        search = new EditText(activity);
        search.setSingleLine(true);
        search.setHint("Buscar modelo o proveedor");
        form.addView(search);
        summary = new TextView(activity);
        form.addView(summary);
        list = new LinearLayout(activity);
        list.setOrientation(LinearLayout.VERTICAL);
        ScrollView scroll = new ScrollView(activity);
        scroll.addView(list);
        form.addView(scroll, new LinearLayout.LayoutParams(-1, (int)(300 * activity.getResources().getDisplayMetrics().density)));
        external = new CheckBox(activity);
        external.setText("Habilitar envíos a los modelos seleccionados");
        external.setChecked(enabled);
        form.addView(external);
        dialog = new AlertDialog.Builder(activity).setTitle("Elegir modelos")
            .setView(form).setNegativeButton("Cancelar", null)
            .setNeutralButton("Proveedores", (d, w) -> openProviders.run())
            .setPositiveButton("Guardar", null).create();
        dialog.setOnShowListener(d -> {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setEnabled(false);
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v -> save());
        });
        dialog.show();
        search.addTextChangedListener(new TextWatcher() {
            @Override public void beforeTextChanged(CharSequence s, int start, int count, int after) { }
            @Override public void onTextChanged(CharSequence s, int start, int before, int count) { render(); }
            @Override public void afterTextChanged(Editable s) { }
        });
        summary.setText("Cargando modelos…");
    }
    private void load() {
        new Thread(() -> {
            try {
                List<LocalModels.Entry> entries = LocalModels.load(activity.getApplicationContext());
                activity.runOnUiThread(() -> {
                    if (!alive()) return;
                    catalog.addAll(entries);
                    // Keep previously selected IDs visible, even if their provider
                    // is currently disconnected, so they can be removed explicitly.
                    for (String id : selection.values()) {
                        if (catalog.stream().noneMatch(e -> e.id.equals(id)))
                            catalog.add(new LocalModels.Entry(id, "Guardado · no disponible ahora"));
                    }
                    loaded = true;
                    dialog.getButton(AlertDialog.BUTTON_POSITIVE).setEnabled(true);
                    render();
                });
            } catch (Exception error) {
                activity.runOnUiThread(() -> {
                    if (!alive()) return;
                    summary.setText("No se pudieron cargar los modelos. Comprobá que el motor esté activo y volvé a abrir Modelos.");
                });
            }
        }, "capimux-models").start();
    }
    private boolean alive() { return !activity.isFinishing() && !activity.isDestroyed() && dialog.isShowing(); }
    private void render() {
        if (!loaded) return;
        list.removeAllViews();
        summary.setText(selection.size() + " de 8 seleccionados");
        String query = search.getText().toString();
        String provider = "";
        int visible = 0;
        for (LocalModels.Entry entry : catalog) {
            if (!ModelSelection.matches(entry.id, entry.provider, query)) continue;
            if (!provider.equals(entry.provider)) {
                provider = entry.provider;
                TextView heading = new TextView(activity);
                heading.setText(provider);
                heading.setPadding(0, 16, 0, 4);
                list.addView(heading);
            }
            CheckBox box = new CheckBox(activity);
            box.setText(entry.label);
            box.setContentDescription(entry.provider + " · " + entry.label);
            box.setChecked(selection.contains(entry.id));
            box.setEnabled(box.isChecked() || selection.size() < ModelSelection.LIMIT);
            box.setOnCheckedChangeListener((button, checked) -> {
                selection.toggle(entry.id, checked);
                // Recreate the visible controls while preserving scroll position.
                ScrollView scroll = (ScrollView) list.getParent();
                int y = scroll.getScrollY();
                render();
                scroll.post(() -> scroll.scrollTo(0, y));
            });
            list.addView(box);
            visible++;
        }
        if (visible == 0) {
            TextView empty = new TextView(activity);
            empty.setText(catalog.isEmpty() ? "Conectá un proveedor para ver sus modelos." : "No hay coincidencias para esa búsqueda.");
            list.addView(empty);
        }
    }
    private void save() {
        if (selection.size() > ModelSelection.LIMIT) {
            Toast.makeText(activity, "Elegí como máximo 8 modelos.", Toast.LENGTH_SHORT).show();
            return;
        }
        try {
            store.save(selection.values(), external.isChecked());
            dialog.dismiss();
            Toast.makeText(activity, "Selección guardada", Toast.LENGTH_SHORT).show();
            openComparator.run();
        } catch (Exception error) {
            Toast.makeText(activity, "No se pudo guardar. Tu selección anterior se conserva.", Toast.LENGTH_LONG).show();
        }
    }
}
