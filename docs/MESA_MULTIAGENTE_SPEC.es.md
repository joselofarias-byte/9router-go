# JolufaSoft - Mesa Multiagente

Fecha: 2026-10-09
Estado: especificacion inicial, sin implementacion

## Necesidad
Un prompt, una tarea y varios modelos seleccionables, ejecutados simultaneamente. Mantener respuestas independientes para comparar, revisar y sintetizar bajo demanda.

## MVP
1. Selector multiple de modelos configurados con capacidades y costo estimado.
2. Envio concurrente acotado, cancelacion y timeout por modelo.
3. Respuestas independientes, streaming, errores parciales, latencia, tokens y costo cuando esten disponibles.
4. Interfaz adaptable a movil: columnas, pestanas, comparacion.
5. Exportacion Markdown/JSON y persistencia SQLite.
6. Segunda ronda opcional: revision cruzada y sintesis manual, sin perder originales.
7. Atribucion por agente y rol: propuesta, implementacion, revision, validacion, coordinacion.

## Integracion
Inspeccionar primero handlers Go, ejecutores de proveedores, rutas existentes, SSE, dashboard Svelte y esquema SQLite. Reutilizar credenciales y enrutamiento; no duplicar autenticacion. API tentativa: POST /api/multiagent/tasks, GET /api/multiagent/tasks/{id}, GET /api/multiagent/tasks/{id}/events, POST /api/multiagent/tasks/{id}/cancel, POST /api/multiagent/tasks/{id}/rounds, GET /api/multiagent/tasks/{id}/export. Ajustar nombres a patrones reales.

## Aceptacion
- Tres modelos procesan simultaneamente un mismo prompt.
- Falla individual no detiene a los demas.
- Cancelacion, timeouts y reconexion comprobados.
- Resultados legibles en Android.
- Sin costos inventados ni secretos en navegador o registros.
- Pruebas automatizadas y sin regresion del proxy existente.

## Atribucion
Idea, necesidad y direccion de producto: usuario responsable de JolufaSoft.
Especificacion y coordinacion inicial: ChatGPT GPT-6.
Implementacion, revision y validacion: pendientes.
No atribuir merito por tareas no ejecutadas.
