Roadmap
Cinco planes están listos para despachar. En este orden, porque cada uno depende del tag del anterior:

#	Repo	Plan	Depende de
1	webtyp/model	✅ listo, 170 líneas	nada
2	webtyp/indexdb	✅ listo, 236 líneas	tag de model
3	webtyp/storage	✅ listo, 147 líneas	tag de indexdb
4	webtyp/vector	✅ listo, 215 líneas	nada (puede ir en paralelo con 1–3)
5	webtyp/vectordb	✅ listo, 263 líneas	tags de storage + vector
Los cinco tienen tests especificados y checklist de aceptación. Son tareas largas — justo para lo que sirve codejob.

Podés despachar hoy: #1 y #4, en paralelo. No dependen de nada ni entre sí.

Lo que NO despaches todavía
Por qué
agent / agentmemory	fase 4, depende de todo lo anterior
tokenizer, weights, embed, transformer	esperan la medición de MFLOPS que sale de #4
Dos cosas chicas que van en sesión, no en codejob
config.json de Granite (1,2 kB) — verifica si son 12 capas × 384. Un minuto.
Migración de module path de agent (github.com/tinywasm/agent → webtyp.com/agent) — mecánica, no necesita un agente.
La fase 0 del plan (round-trip de bytes, auto-commit de IndexedDB) también es chica, pero el plan de indexdb ya la cubre en su §0, así que se resuelve dentro del despacho #2.

El camino completo, en una línea: despachás 1 y 4 → cuando #4 devuelve MFLOPS, dividís 856 entre ese número → eso decide si las fases 3 existen o si vas a tabla estática. Todo lo demás son consecuencias de esa división.