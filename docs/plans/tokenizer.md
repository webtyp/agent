---
PLAN: "feat: webtyp/tokenizer — de texto a ids de tokens"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/tokenizer (por crear)
---

> Repositorio nuevo. Se mueve a `tokenizer/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/tokenizer`

## Responsabilidad única

Entra texto, salen ids de tokens. Nada más. Sin embeddings, sin pesos de modelo, sin GPU,
sin almacenamiento. Go puro, cero dependencias, compila en todas partes, enteramente
testeable sin navegador.

Está separado de `embed` porque ambas fases del embedder lo necesitan de forma idéntica —
una tabla de embeddings estática y un encoder transformer tokenizan exactamente igual — y
porque la tokenización es donde viven los bugs sutiles y difíciles de encontrar. Merece su
propia suite de tests y su propio versionado.

## Por qué tiene que ser exacto

Un tokenizador que discrepa del usado para entrenar el modelo produce embeddings
*plausibles pero incorrectos*: sin crash, sin error, apenas una recuperación degradada en
silencio que parece un modelo malo. Cada test de la sección §Tests existe para fijar
comportamiento contra la implementación de referencia, no para comprobar que el código
corre.

## Alcance

**v1: WordPiece** (familia BERT, que cubre los candidatos multilingües de
sentence-transformer de `plans/embed.md`).

**v2: Unigram/SentencePiece**, si el modelo elegido lo necesita. Decidir recién cuando se
resuelva `plans/embed.md` §2 — implementar los dos por adelantado es especulativo.

BPE queda fuera de alcance: ningún modelo candidato lo usa.

## API

```go
// Tokenizer turns text into model input ids.
type Tokenizer interface {
	// Encode appends token ids for text into dst and returns it. The caller owns
	// dst, so a loop over many texts allocates once.
	Encode(dst []int32, text string) []int32

	// Decode is for debugging and tests only; it is not on any hot path.
	Decode(ids []int32) string

	VocabSize() int
	MaxLen() int
}

type Config struct {
	Vocab        map[string]int32 // loaded from the model artifact
	Lowercase    bool
	StripAccents bool             // note: NOT the same as Lowercase — see below
	MaxLen       int              // truncate; 0 = unlimited
	UnkToken     string           // "[UNK]"
	ClsToken     string           // "[CLS]" — prepended when non-empty
	SepToken     string           // "[SEP]" — appended when non-empty
}

func NewWordPiece(cfg Config) (Tokenizer, error)
```

`Encode` recibe un slice destino en vez de devolver uno nuevo porque embeber un lote de
1024 documentos asignaría, si no, 1024 slices.

## Notas de implementación

El pipeline es: normalizar → pre-tokenizar por espacios y puntuación → WordPiece voraz de
coincidencia más larga por palabra → agregar tokens especiales → truncar.

Tres detalles que suelen salir mal:

1. **Quitar acentos no es pasar a minúsculas.** Un modelo multilingüe para español
   típicamente tendrá `strip_accents = false`, porque *ánimo* y *animo* son palabras
   distintas. Invertir esto degrada en silencio exactamente el idioma que a este proyecto
   le importa (índice maestro **D5**). El flag es explícito y separado por esa razón, y su
   valor por defecto tiene que venir del artifact del modelo, nunca de una constante acá.

2. **Unicode sin `golang.org/x/text`.** La normalización NFD y el corte de puntuación por
   categoría requieren cuidado bajo TinyGo, donde las tablas de `unicode` inflan el
   binario. Medí el impacto en el binario WASM antes de comprometerte con un enfoque basado
   en tablas; una tabla reducida cubriendo latín, puntuación y rangos CJK puede ser el
   compromiso correcto. Registrá la medición en el README.

3. **La coincidencia voraz más larga es sobre *bytes* después de normalizar**, y una
   palabra que no se puede segmentar se convierte en un único `[UNK]` — no en una secuencia
   de `[UNK]` por carácter.

## Tests

Los fixtures son el entregable acá. Generalos una vez con el tokenizador de referencia en
Python, commiteálos como `testdata/*.json`, y fijá contra ellos para siempre.

| Test | Verifica |
|---|---|
| `TestEncode_MatchesReferenceFixtures` | cada par de fixture codifica idénticamente — el único test que realmente importa |
| `TestEncode_Spanish` | palabras acentuadas, `ñ`, `¿¡`, con `StripAccents` encendido y apagado |
| `TestEncode_UnknownWord` | un `[UNK]`, no uno por carácter |
| `TestEncode_SpecialTokens` | ubicación de `[CLS]`/`[SEP]`, y su ausencia cuando no están configurados |
| `TestEncode_Truncation` | `MaxLen` trunca y aun así cierra con `[SEP]` |
| `TestEncode_EmptyString` | solo tokens especiales, sin pánico |
| `TestEncode_ReusesDst` | una segunda llamada agrega al slice del llamador sin reasignar |
| `TestEncode_ZeroAllocsWithCapacity` | `testing.AllocsPerRun` == 0 cuando `dst` tiene capacidad |
| `TestDecode_RoundTrip` | para texto que tokeniza limpio |
| `TestEncode_Emoji` | las secuencias multibyte no se parten a mitad de rune |

## Checklist de aceptación

```bash
go vet ./...
gotest
ls testdata/*.json                        # fixtures commiteados
GOOS=js GOARCH=wasm go build ./...
grep -rn "golang.org/x/" .                # → vacío, o justificado en el README
```
