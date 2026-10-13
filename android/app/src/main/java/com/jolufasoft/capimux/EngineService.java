package com.jolufasoft.capimux;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;

import java.io.File;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.URL;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

public final class EngineService extends Service {
    private static final String CHANNEL = "capimux-engine";
    private static final int NOTIFICATION_ID = 31;
    private static final Object LOCK = new Object();
    private static volatile Process child;
    private static volatile boolean launching;
    private static volatile boolean ready;
    private static volatile String status = "Detenido";
    private static long epoch;
    private final ExecutorService workers = Executors.newCachedThreadPool();
    private BoundedLog engineLog;
    private long generation;

    public static String status() { return status; }
    public static boolean ready() { return ready; }
    public static boolean idle() { return child == null && !launching; }

    public static void start(Context context) {
        Intent intent = new Intent(context, EngineService.class);
        context.startForegroundService(intent);
    }

    @Override public void onCreate() {
        super.onCreate();
        engineLog = new BoundedLog(new File(getFilesDir(), "engine.log"));
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(CHANNEL, "Motor de Capimux", NotificationManager.IMPORTANCE_LOW));
        startForeground(NOTIFICATION_ID, notification("Preparando el motor"));
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        synchronized (LOCK) {
            if (launching || child != null) return START_NOT_STICKY;
            generation = ++epoch;
            launching = true;
            ready = false;
            status = "Iniciando…";
        }
        workers.execute(() -> launch(generation));
        return START_NOT_STICKY;
    }

    private void launch(long token) {
        Process process = null;
        try {
            File data = new File(getFilesDir(), "engine");
            if (!data.exists() && !data.mkdirs()) throw new IllegalStateException("No se pudo crear el directorio de datos.");
            try (ServerSocket probe = new ServerSocket()) {
                probe.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), NavigationPolicy.PORT));
            }
            File binary = new File(getApplicationInfo().nativeLibraryDir, "libcapimux.so");
            if (!binary.canExecute()) throw new IllegalStateException("El motor no está instalado para esta arquitectura.");
            ProcessBuilder builder = new ProcessBuilder(binary.getAbsolutePath());
            builder.directory(data);
            builder.redirectErrorStream(true);
            Map<String, String> env = builder.environment();
            env.put("HOME", getFilesDir().getAbsolutePath());
            env.put("DATA_DIR", data.getAbsolutePath());
            env.put("TMPDIR", getCacheDir().getAbsolutePath());
            env.put("HOST", "127.0.0.1");
            env.put("PORT", Integer.toString(NavigationPolicy.PORT));
            env.put("AUTO_UPDATE", "false");
            env.put("RTK_ENABLED", "false");
            env.remove("LOG_FILE");
            new ModelStore(this).migrate(this);
            env.put("CAPIMUX_MULTIAGENT_POLICY_FILE", ModelStore.path(this).getAbsolutePath());
            env.remove("CAPIMUX_MULTIAGENT_ALLOWED_MODELS");
            env.remove("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL");
            process = builder.start();
            synchronized (LOCK) {
                if (token != epoch) {
                    process.destroyForcibly();
                    return;
                }
                child = process;
                launching = false;
            }
            final Process running = process;
            workers.execute(() -> drain(running));
            workers.execute(() -> awaitReady(running, token));
            int code = running.waitFor();
            synchronized (LOCK) {
                if (child == running) child = null;
                if (token != epoch) return;
                ready = false;
                status = "Motor detenido (código " + code + "). Revisá el registro.";
            }
            stopSelf();
        } catch (Exception e) {
            synchronized (LOCK) {
                if (token != epoch) return;
                ready = false;
                status = "No se pudo iniciar: " + e.getClass().getSimpleName() + ". Revisá el registro.";
            }
            try {
                byte[] message = (e.toString() + "\n").getBytes(java.nio.charset.StandardCharsets.UTF_8);
                log().append(message, message.length);
            } catch (Exception ignored) { }
            if (process != null) process.destroy();
            stopSelf();
        } finally {
            synchronized (LOCK) { if (child == null) launching = false; }
        }
    }

    private void drain(Process running) {
        byte[] buffer = new byte[4096];
        try (InputStream input = running.getInputStream()) {
            int count;
            while ((count = input.read(buffer)) != -1) log().append(buffer, count);
        } catch (Exception ignored) { }
    }

    private void awaitReady(Process running, long token) {
        for (int attempt = 0; attempt < 60 && running.isAlive(); attempt++) {
            synchronized (LOCK) { if (token != epoch) return; }
            if (healthy()) {
                synchronized (LOCK) {
                    if (token != epoch || !running.isAlive()) return;
                    ready = true;
                    status = "Activo · " + NavigationPolicy.ORIGIN;
                }
                getSystemService(NotificationManager.class).notify(NOTIFICATION_ID, notification("Motor activo en este teléfono"));
                return;
            }
            try { Thread.sleep(500); } catch (InterruptedException e) { Thread.currentThread().interrupt(); return; }
        }
        synchronized (LOCK) {
            if (token != epoch) return;
            status = "El motor no respondió. Revisá el registro.";
        }
        stopSelf();
    }

    private boolean healthy() {
        HttpURLConnection connection = null;
        try {
            connection = (HttpURLConnection) new URL(NavigationPolicy.ORIGIN + "/health").openConnection();
            connection.setConnectTimeout(500);
            connection.setReadTimeout(500);
            return connection.getResponseCode() == 200;
        } catch (Exception e) { return false; }
        finally { if (connection != null) connection.disconnect(); }
    }

    private BoundedLog log() { return engineLog; }

    private Notification notification(String text) {
        PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        return new Notification.Builder(this, CHANNEL).setSmallIcon(R.drawable.ic_capimux)
            .setContentTitle("Capimux").setContentText(text).setContentIntent(open).setOngoing(true).build();
    }

    @Override public void onDestroy() {
        Process stopping;
        synchronized (LOCK) {
            ++epoch;
            ready = false;
            stopping = child;
            if (stopping != null || launching) status = "Deteniendo…";
        }
        if (stopping != null) {
            stopping.destroy();
            workers.execute(() -> {
                try {
                    if (!stopping.waitFor(20, TimeUnit.SECONDS)) stopping.destroyForcibly();
                    stopping.waitFor();
                } catch (InterruptedException e) { stopping.destroyForcibly(); Thread.currentThread().interrupt(); }
                synchronized (LOCK) {
                    if (child == stopping) child = null;
                    status = "Detenido";
                }
            });
        }
        workers.shutdown();
        stopForeground(STOP_FOREGROUND_REMOVE);
        super.onDestroy();
    }

    @Override public IBinder onBind(Intent intent) { return null; }
}
