# Comparador multimodelo de Capimux

La pestaña Comparador permite enviar una tarea a hasta ocho modelos y muestra cada respuesta por separado, en el orden seleccionado. Un fallo de un modelo no elimina las otras respuestas.

## Configuración del servidor

- Configurá primero las conexiones de proveedores en el dashboard.
- Definí CAPIMUX_MULTIAGENT_ALLOWED_MODELS con los identificadores exactos de tus modelos, separados por coma. El selector muestra exclusivamente esta lista; no presupone modelos o proveedores disponibles.
- Definí CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL=1 para habilitar envíos. Sin esa variable solo funciona la simulación.
- Iniciá sesión en el dashboard. Las rutas del comparador siempre exigen sesión administrativa o el token CLI local, incluso con requireLogin desactivado. Las claves de inferencia no habilitan estas operaciones.

Seleccioná modelos, escribí la tarea y validá la selección. Para obtener respuestas, desmarcá la simulación, seleccioná el envío a proveedores y pulsá Ejecutar en paralelo.

## Límites y resultados

Máximo ocho modelos por lote, tres llamadas simultáneas por defecto y dos lotes activos en el proceso. Cada llamada dispone de 60 segundos por defecto, contados desde su inicio, y solicita como máximo 4096 tokens de salida. Se limita a 1 MiB el buffer del adaptador por respuesta. Estos límites no constituyen un presupuesto monetario.

Cancelar o salir de la vista aborta la solicitud y propaga la cancelación al proveedor. Un proveedor puede haber procesado trabajo antes de recibirla.

El envío reutiliza el handler de chat: conserva selección de proveedor, rotación de cuentas y registro de uso. El comparador solicita respuestas sin streaming y no ejecuta herramientas devueltas por los modelos. Contenido vacío, timeout y respuesta excesiva aparecen como errores individuales. El endpoint de política y la simulación no prueban conectividad ni saldo del proveedor.

## Verificación

Las pruebas de integración usan el router y la base de datos reales con proveedores HTTP simulados: verifican autenticación, selección exacta, simulación sin llamadas y dos respuestas independientes. Las pruebas unitarias comprueban concurrencia, cancelación antes del envío y límites de respuesta. No requieren credenciales de proveedores reales.

Esta funcionalidad es una extensión del fork; no modifica los contratos de inferencia de decolua/9router.
