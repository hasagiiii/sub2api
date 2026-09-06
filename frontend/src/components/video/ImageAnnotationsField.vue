<template>
  <div class="min-w-0 space-y-3" data-test="image-annotations">
    <ImageUrlsField :model-value="images" :max-items="maxItems || 10" :disabled="disabled" media-kind="image" @update:model-value="changeImages" />
    <template v-if="images.length">
      <div class="flex flex-wrap items-center gap-2">
        <select v-model="activeURL" class="input h-9 min-w-0 flex-1" :aria-label="text.image" :disabled="disabled">
          <option v-for="(url, i) in images" :key="url" :value="url">{{ text.image }} {{ i + 1 }}</option>
        </select>
        <div class="flex gap-1" role="toolbar" :aria-label="text.annotations">
          <button v-for="tool in tools" :key="tool.mode" type="button" class="annotation-tool" :class="{ active: mode === tool.mode }" :title="tool.title" :aria-label="tool.title" :aria-pressed="mode === tool.mode" :disabled="disabled || !loaded" @click="mode = tool.mode">
            <Icon :name="tool.icon" size="sm" />
          </button>
          <button type="button" class="annotation-tool" :title="text.undo" :aria-label="text.undo" :disabled="disabled || !loaded || !undoStack.length" @click="undo"><Icon name="arrowLeft" size="sm" /></button>
          <button type="button" class="annotation-tool" :title="text.redo" :aria-label="text.redo" :disabled="disabled || !loaded || !redoStack.length" @click="redo"><Icon name="arrowRight" size="sm" /></button>
          <button type="button" class="annotation-tool" :title="text.remove" :aria-label="text.remove" :disabled="disabled || !selected" @click="removeSelected"><Icon name="trash" size="sm" /></button>
        </div>
      </div>
      <div ref="viewport" class="relative h-[360px] w-full overflow-auto border border-gray-300 bg-gray-100 dark:border-gray-700 dark:bg-gray-900">
        <div ref="canvas" class="touch-none" />
        <span v-if="!loaded" class="absolute inset-0 flex items-center justify-center px-3 text-center text-sm text-gray-500">{{ loadError ? text.loadError : text.loading }}</span>
      </div>
      <div class="flex items-center gap-3 text-xs">
        <label class="flex min-w-0 flex-1 items-center gap-2">{{ text.zoom }}<input v-model.number="zoom" type="range" min="1" max="3" step="0.1" class="min-w-0 flex-1" :disabled="disabled || !loaded" /></label>
        <output class="w-12 text-right">{{ Math.round(zoom * 100) }}%</output>
      </div>
      <div v-if="selectedAnnotation" class="space-y-2">
        <div class="flex flex-wrap gap-2">
          <label v-for="(value, i) in selectedAnnotation.points" :key="i" class="flex items-center gap-1 text-xs">
            {{ coordinateLabels[i] }}
            <input type="number" min="0" max="1000" class="input h-8 w-20" :value="value" :disabled="disabled" @change="changeCoordinate(i, Number(($event.target as HTMLInputElement).value))" />
          </label>
        </div>
        <textarea :value="selectedAnnotation.instruction" :placeholder="text.instruction" :aria-label="text.instruction" :disabled="disabled" rows="2" class="input w-full" @change="changeInstruction(($event.target as HTMLTextAreaElement).value)" />
      </div>
      <details v-if="prompt" class="text-xs"><summary>{{ text.preview }}</summary><pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words">{{ prompt }}</pre></details>
      <button type="button" class="btn btn-secondary" :disabled="disabled || !loaded" @click="emit('apply', prompt)"><Icon name="check" size="sm" />{{ text.apply }}</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Konva from 'konva'
import ImageUrlsField from './ImageUrlsField.vue'
import { Icon } from '@/components/icons'
import { annotationPrompt, clampCoordinate, normalizeAnnotation, readAnnotationDocument, type AnnotationDocument, type ImageAnnotation } from './imageAnnotations'

const props = defineProps<{ modelValue: unknown; annotations?: AnnotationDocument; disabled?: boolean; maxItems?: number }>()
const emit = defineEmits<{ 'update:modelValue': [string[]]; 'update:annotations': [AnnotationDocument]; apply: [string] }>()
const { locale } = useI18n()
const text = computed(() => locale.value.startsWith('zh') ? { image: '图片', annotations: '标注', select: '选择与移动', point: '坐标点', box: '矩形框', arrow: '箭头', undo: '撤销', redo: '重做', remove: '删除标注', zoom: '缩放', loading: '正在加载图片', loadError: '图片加载失败', instruction: '编辑指令', preview: '提示词预览', apply: '应用到提示词' } : { image: 'Image', annotations: 'Annotations', select: 'Select and move', point: 'Point', box: 'Rectangle', arrow: 'Arrow', undo: 'Undo', redo: 'Redo', remove: 'Delete annotation', zoom: 'Zoom', loading: 'Loading image', loadError: 'Image failed to load', instruction: 'Editing instruction', preview: 'Prompt preview', apply: 'Apply to prompt' })
type Mode = 'select' | ImageAnnotation['kind']
const tools = computed(() => [{ mode: 'select' as Mode, icon: 'edit' as const, title: text.value.select }, { mode: 'point' as Mode, icon: 'plus' as const, title: text.value.point }, { mode: 'box' as Mode, icon: 'grid' as const, title: text.value.box }, { mode: 'arrow' as Mode, icon: 'arrowRight' as const, title: text.value.arrow }])
const coordinateLabels = ['x1', 'y1', 'x2', 'y2']
const images = computed<string[]>(() => typeof props.modelValue === 'string' ? [props.modelValue].filter(Boolean) : Array.isArray(props.modelValue) ? props.modelValue.filter((v): v is string => typeof v === 'string' && !!v) : [])
const document = computed(() => readAnnotationDocument(props.annotations))
const activeURL = ref('')
const selected = ref('')
const mode = ref<Mode>('box')
const zoom = ref(1)
const loaded = ref(false)
const loadError = ref(false)
const viewport = ref<HTMLDivElement>()
const canvas = ref<HTMLDivElement>()
const undoStack = ref<AnnotationDocument[]>([])
const redoStack = ref<AnnotationDocument[]>([])
const selectedAnnotation = computed(() => (document.value[activeURL.value] || []).find(a => a.id === selected.value))
const prompt = computed(() => annotationPrompt(images.value, document.value))
let stage: Konva.Stage | undefined
let layer: Konva.Layer | undefined
let group: Konva.Group | undefined
let image: HTMLImageElement | undefined
let observer: ResizeObserver | undefined
let loadVersion = 0
let draft: ImageAnnotation | undefined

function commit(next: AnnotationDocument) {
  undoStack.value.push(structuredClone(document.value)); if (undoStack.value.length > 100) undoStack.value.shift()
  redoStack.value = []; emit('update:annotations', next)
}
function undo() { const next = undoStack.value.pop(); if (next) { redoStack.value.push(structuredClone(document.value)); emit('update:annotations', next) } }
function redo() { const next = redoStack.value.pop(); if (next) { undoStack.value.push(structuredClone(document.value)); emit('update:annotations', next) } }
function updateAnnotation(annotation: ImageAnnotation) {
  const items = document.value[activeURL.value] || []
  const next = normalizeAnnotation(annotation)
  commit({ ...document.value, [activeURL.value]: items.some(a => a.id === next.id) ? items.map(a => a.id === next.id ? next : a) : [...items, next] })
}
function removeSelected() { commit({ ...document.value, [activeURL.value]: (document.value[activeURL.value] || []).filter(a => a.id !== selected.value) }); selected.value = '' }
function changeInstruction(instruction: string) { if (selectedAnnotation.value) updateAnnotation({ ...selectedAnnotation.value, instruction }) }
function changeCoordinate(index: number, value: number) { if (!selectedAnnotation.value || !Number.isFinite(value)) return; const points = [...selectedAnnotation.value.points]; points[index] = value; updateAnnotation({ ...selectedAnnotation.value, points }) }
function changeImages(next: string[]) {
  const retained = Object.fromEntries(Object.entries(document.value).filter(([url]) => next.includes(url)))
  undoStack.value = []; redoStack.value = []
  emit('update:modelValue', next); emit('update:annotations', retained)
  emit('apply', annotationPrompt(next, retained))
}
function position() {
  const p = stage?.getPointerPosition(); if (!p || !group) return null
  const point = group.getAbsoluteTransform().copy().invert().point(p)
  return { x: clampCoordinate(point.x), y: clampCoordinate(point.y) }
}
function draw() {
  if (!stage || !layer || !image || !loaded.value || !viewport.value) return
  layer.destroyChildren()
  const width = Math.max(200, viewport.value.clientWidth)
  const fit = Math.min(width / image.naturalWidth, 360 / image.naturalHeight)
  const w = image.naturalWidth * fit * zoom.value, h = image.naturalHeight * fit * zoom.value
  stage.size({ width: Math.max(width, w), height: Math.max(360, h) })
  group = new Konva.Group({ x: (stage.width() - w) / 2, y: (stage.height() - h) / 2, scaleX: w / 1000, scaleY: h / 1000 })
  layer.add(group)
  group.add(new Konva.Image({ image, width: 1000, height: 1000, listening: false }))
  const items = [...(document.value[activeURL.value] || []), ...(draft ? [draft] : [])]
  for (const a of items) {
    const p = normalizeAnnotation(a).points
    const common = { id: a.id, name: 'annotation', stroke: a.id === selected.value ? '#e11d48' : '#0891b2', strokeWidth: 2, strokeScaleEnabled: false, hitStrokeWidth: 15, draggable: !props.disabled && mode.value === 'select' }
    let shape: Konva.Shape
    if (a.kind === 'box') shape = new Konva.Rect({ ...common, x: p[0], y: p[1], width: p[2] - p[0], height: p[3] - p[1] })
    else if (a.kind === 'point') shape = new Konva.Circle({ ...common, x: p[0], y: p[1], radius: 10, fill: '#0891b2' })
    else shape = new Konva.Arrow({ ...common, points: p, pointerLength: 18, pointerWidth: 14, fill: '#0891b2' })
    shape.on('click tap', () => { if (!props.disabled && mode.value === 'select') { selected.value = a.id; draw() } })
    shape.on('dragend transformend', () => {
      const points = a.kind === 'box' ? [shape.x(), shape.y(), shape.x() + shape.width() * shape.scaleX(), shape.y() + shape.height() * shape.scaleY()] : a.kind === 'point' ? [shape.x(), shape.y()] : a.points.map((v, i) => v + (i % 2 ? shape.y() : shape.x()))
      updateAnnotation({ ...a, points })
    })
    group.add(shape)
    if (a.id === selected.value && a.kind === 'box' && mode.value === 'select' && !props.disabled) {
      const transformer = new Konva.Transformer({ nodes: [shape], rotateEnabled: false, flipEnabled: false, keepRatio: false, anchorSize: 12 })
      layer.add(transformer)
    }
  }
  layer.draw()
}
async function loadImage() {
  const version = ++loadVersion; loaded.value = false; loadError.value = false; image = undefined
  layer?.destroyChildren(); layer?.draw(); selected.value = ''; draft = undefined; zoom.value = 1
  if (!activeURL.value) return
  const next = new Image()
  next.onload = () => { if (version !== loadVersion) return; image = next; loaded.value = true; draw() }
  next.onerror = () => { if (version === loadVersion) loadError.value = true }
  next.src = activeURL.value
}
watch(images, async next => {
  if (!next.includes(activeURL.value)) activeURL.value = next[0] || ''
  if (!next.length) { observer?.disconnect(); stage?.destroy(); stage = undefined; layer = undefined; group = undefined }
  await nextTick(); initialize()
}, { immediate: true })
watch(activeURL, loadImage)
watch([document, mode, zoom, () => props.disabled], () => draw(), { deep: true })
function initialize() {
  if (stage || !canvas.value) return
  stage = new Konva.Stage({ container: canvas.value, width: 300, height: 360 }); layer = new Konva.Layer(); stage.add(layer)
  stage.on('mousedown touchstart', event => {
    if (props.disabled || !loaded.value || mode.value === 'select') return
    event.evt.preventDefault()
    const p = position(); if (!p) return
    draft = { id: crypto.randomUUID(), kind: mode.value, points: mode.value === 'point' ? [p.x, p.y] : [p.x, p.y, p.x, p.y], instruction: '' }
    selected.value = draft.id; draw()
  })
  stage.on('mousemove touchmove', event => { if (!draft || draft.kind === 'point') return; event.evt.preventDefault(); const p = position(); if (p) { draft.points[2] = p.x; draft.points[3] = p.y; draw() } })
  stage.on('mouseup touchend', () => { if (!draft) return; const result = draft; draft = undefined; if (result.kind === 'point' || Math.hypot(result.points[2] - result.points[0], result.points[3] - result.points[1]) > 2) updateAnnotation(result); draw() })
  observer = new ResizeObserver(draw); if (viewport.value) observer.observe(viewport.value)
  void loadImage()
}
onMounted(initialize)
onBeforeUnmount(() => { loadVersion++; observer?.disconnect(); stage?.destroy() })
</script>

<style scoped>
.annotation-tool { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; border: 1px solid #9ca3af; border-radius: 4px; }
.annotation-tool.active { color: #0f766e; background: #ccfbf1; border-color: #0d9488; }
.annotation-tool:disabled { opacity: .4; cursor: not-allowed; }
</style>
