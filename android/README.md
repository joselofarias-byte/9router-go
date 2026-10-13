# Capimux para Android — beta 0.1.0

App Android independiente con el motor Go y su dashboard Svelte incluidos. No requiere Termux, PRoot, root ni una descarga posterior del motor.

## Uso

Al abrir la app inicia el motor y carga el panel local. Iniciar, Detener y Recargar controlan el servicio. Modelos configura los identificadores permitidos para el comparador y su habilitación; al guardar, el motor se reinicia conservando la base de datos. Registro muestra las últimas líneas y Batería abre los ajustes del teléfono.

En una instalación nueva, la clave inicial del panel es 123456, siguiendo el contrato del motor. Puede cambiarse desde el panel.

El motor escucha exclusivamente en http://127.0.0.1:20131. Otros clientes del teléfono pueden usar ese destino con una clave de inferencia creada en el panel. El puerto es distinto del 20130 de Termux para permitir probar ambas instalaciones por separado.

Las cuentas, claves y base de datos se guardan en el espacio privado de esta app. No importa automáticamente archivos de Termux. Los respaldos automáticos de Android están desactivados. La app no contiene credenciales de proveedores.

## Ciclo de vida

El motor se ejecuta desde nativeLibraryDir como ejecutable PIE incluido en el APK. No se copia código ejecutable al directorio de datos. El proceso está supervisado por un servicio en primer plano de tipo specialUse y una notificación. Al detenerlo se solicita cierre ordenado y se espera hasta 20 segundos antes de forzar su finalización. El registro se limita a 256 KiB.

El panel solo carga recursos del origen local. Los enlaces web externos, incluidos flujos OAuth compatibles, se abren en el navegador del sistema. No se añade un puente JavaScript con acceso a archivos, tokens o ejecución de comandos.

Android o MagicOS pueden cerrar la app por restricciones del sistema. Esta beta no arranca automáticamente al reiniciar el teléfono. Las funciones que necesitan CLIs externos no incluyen esos programas; API keys y OAuth web usan el motor existente.

La actualización automática del ejecutable está desactivada: el motor se actualiza instalando un APK firmado con la misma clave. La firma de la beta distribuida se conserva en el repositorio privado, nunca en este repositorio público. Los artefactos debug de CI usan una firma de prueba y no son la beta distribuida.

El icono de esta primera beta es un marcador temporal con la letra C; la identidad gráfica definitiva queda pendiente.

## Construcción

Requisitos: Go 1.27, Bun 1.4.2, JDK 17, Gradle 8.11.1, SDK 35 y NDK 28.0.13004108.

La compilación APK usa el tag capimux_apk y SQLite mediante bionic. Las compilaciones normales conservan el driver pure-Go existente. La alternativa APK evita las llamadas legacy de libc que el sandbox Android bloquea en x86_64; también se prueba ese adaptador con la suite de base de datos en el host.

Construí web/dist, ejecutá bash android/ci/build-engine.sh y luego gradle -p android :app:assembleHonorDebug. La variante honor incluye ARM64; emulator incluye x86_64 para pruebas.

CI ejecuta pruebas Java de navegación y registro, lint, compilación nativa y una prueba en emulador Android 16 que comprueba arranque, parada, reinicio, persistencia de datos y servicio con la app en segundo plano. El emulador no sustituye la validación física en HONOR.
