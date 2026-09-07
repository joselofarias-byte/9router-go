# 9router Go: Termux nativo (ARM64)

## Corrección fabric.4: ruta Grok

El adaptador normaliza las bases raíz y /v1 a /v1/responses y añade
X-XAI-Token-Auth y x-grok-model-override. Las rutas personalizadas completas
se conservan. Una prueba local verifica ruta, encabezados y propagación de
errores sin contactar proveedores. Los probes ya no presentan todo 404 como
model_not_found: esa condición también puede indicar una ruta inexistente.

## Corrección fabric.3: DNS Android

Las compilaciones fabric.2 desactivaban CGO: podían iniciar y atender en
localhost pero fallar al resolver proveedores. fabric.3 compila con CGO y
netcgo para usar getaddrinfo de Bionic. GitHub compila con el Android NDK;
en Termux se necesita clang además de Go si se compila localmente.
No se fuerza un DNS público ni se cambia la configuración de red del usuario.

El paquete incluye 9router-netcheck-android-arm64. Ejecutarlo antes de
reemplazar el gateway: comprueba DNS y TLS de los dos hosts de Grok sin
enviar credenciales, solicitudes HTTP ni prompts. Debe terminar con código
0. Esta comprobación debe realizarse en el teléfono; compilar no demuestra
conectividad real ni validez de cuentas.

Esta entrega es un ejecutable Android ARM64, no una APK. Se ejecuta dentro
de Termux sin iniciar una distribución en PRoot. Una APK requeriría un
proyecto Android separado con servicio, interfaz y ciclo de vida propios.

## Descargar e instalar

En GitHub, abrir Actions → Fabric - Termux ARM64 → ejecución correcta →
Artifacts → 9router-go-termux-arm64. Es necesario iniciar sesión para
descargar el ZIP de Actions; el artefacto se conserva 30 días.

Extraer el ZIP en un directorio privado de Termux y ejecutar desde allí:

```sh
sha256sum -c SHA256SUMS
mkdir -p "$HOME/.local/bin"
install -m 700 9router-go-android-arm64 "$HOME/.local/bin/9router-go"
"$HOME/.local/bin/9router-go" --help
```

No ejecutar directamente desde almacenamiento compartido/Download.
Antes de sustituir una instalación existente, detener el proceso anterior
y respaldar su base de datos. Mantener su configuración y ruta de datos;
este paquete no migra automáticamente una instalación dentro de PRoot.

La administración ahora exige `NINEROUTER_ADMIN_TOKEN` de al menos 32
caracteres, enviado como `Authorization: Bearer ...`. Generar el secreto
localmente y conservarlo fuera del repositorio. Las claves de inferencia
siguen siendo independientes. Los clientes administrativos y el dashboard
necesitan enviar este token; su integración no está incluida en este parche.

## Compilar desde el código

Con Go compatible con go.mod instalado, ejecutar:

```sh
bash scripts/build-termux.sh
```

El script también permite compilar Android ARM64 desde Linux. La salida
está en dist/termux. No hace falta compilar en el teléfono si se descarga
el artefacto de Actions. La compilación se ha verificado; la ejecución en
un dispositivo Android real todavía necesita validación.

## Alcance del parche Fabric

- Filtra cuentas activas y compatibles, respeta cooldown y prioridad.
- Aplica rotación y fallback; evita reintentar una respuesta ya iniciada.
- Separa autenticación administrativa de las claves de inferencia.
- Añade /admin/fabric/accounts, rotation, probe, state, apply y rollback.
- Mantiene snapshots experimentales con hash, caducidad y control de
  concurrencia. Conserva el último aplicado si el controlador no responde.
- Reduce contenido sensible en registros y exige firma Ed25519 para
  reemplazar binarios mediante el actualizador. Sin clave pública de
  confianza y firma válida, la actualización automática se rechaza.

El controlador Fabric sigue siendo un proyecto/proceso separado. Este
repositorio contiene el gateway y su contrato administrativo. Las
credenciales de SQLite conservan el formato previo; no se añade cifrado.
Las pruebas de CI de esta rama son regresiones aisladas de Fabric y errores
de upstream; no se ejecutan pruebas heredadas que contactan proveedores.

## Iniciar una instalación nueva

Configurar el token administrativo en el entorno antes de iniciar. Para una
instalación nueva, elegir una carpeta privada y ejecutar:

```sh
DATA_DIR="$HOME/.local/share/9router-go" PORT=20128 "$HOME/.local/bin/9router-go"
```

Para una actualización, usar la ruta de datos anterior. No crear una base
vacía por accidente: las cuentas no aparecen sin su base de datos existente.
