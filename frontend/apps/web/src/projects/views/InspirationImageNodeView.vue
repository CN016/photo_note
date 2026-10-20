<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NodeViewWrapper, type NodeViewProps } from '@tiptap/vue-3'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { api } from '../../api/client'

const props = defineProps<NodeViewProps>()
const { t } = useI18n({ useScope: 'global' })
const url = ref('')
const loading = ref(false)
const failed = ref(false)
const menuOpen = ref(false)
const width = computed(() => Math.max(30, Math.min(100, Number(props.node.attrs.width) || 100)))
let requestVersion = 0

/** 从 inspiration:// 业务 ID 读取当前账号的灵感图，并延迟到节点挂载后下载实际图像。 */
async function load() {
  const current = ++requestVersion
  const inspirationID = String(props.node.attrs.src ?? '').replace(/^inspiration:\/\//, '')
  if (!inspirationID) { failed.value = true; return }
  loading.value = true
  failed.value = false
  try {
    const inspiration = await api.inspiration(inspirationID)
    const blob = await api.download(inspiration.image_key)
    if (current === requestVersion) url.value = URL.createObjectURL(blob)
  } catch { if (current === requestVersion) failed.value = true }
  finally { if (current === requestVersion) loading.value = false }
}

function setWidth(event: Event) { props.updateAttributes({ width: Number((event.target as HTMLInputElement).value) }) }
function setSmall() { props.updateAttributes({ width: 33, layout: 'small' }); menuOpen.value = false }
function setOriginal() { props.updateAttributes({ width: 100, layout: 'original' }); menuOpen.value = false }
function remove() { props.deleteNode() }
function moveNode(direction: -1 | 1) {
  const position = props.getPos()
  if (typeof position !== 'number') return
  const document = props.editor.state.doc
  const current = document.nodeAt(position)
  if (!current) return
  const previous = direction < 0 ? document.resolve(position).nodeBefore : null
  if (direction < 0 && !previous) return
  const siblingPosition = direction < 0 ? position - previous!.nodeSize : position + current.nodeSize
  const sibling = document.nodeAt(siblingPosition)
  if (!sibling) return
  const transaction = props.editor.state.tr.delete(position, position + current.nodeSize)
  const target = direction < 0 ? position - sibling.nodeSize : position + sibling.nodeSize
  props.editor.view.dispatch(transaction.insert(target, current).scrollIntoView())
}

onMounted(() => { void load() })
watch(() => props.node.attrs.src, () => {
  requestVersion += 1
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  void load()
})
onBeforeUnmount(() => { requestVersion += 1; if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template>
  <NodeViewWrapper class="project-resource-node inspiration-node" :class="{ 'is-selected': selected }" :style="{ width: `${width}%` }" @contextmenu.prevent="menuOpen = !menuOpen">
    <figure class="project-resource-frame">
      <img v-if="url" :src="url" :alt="t('projects.toolbar.insertInspiration')" draggable="false" />
      <span v-else class="resource-placeholder">{{ failed ? t('common.errors.not_found') : t('common.loading') }}</span>
      <figcaption>{{ t('projects.dragHint') }}</figcaption>
    </figure>
    <div class="resource-controls" contenteditable="false">
      <el-button size="small" @mousedown.prevent @click="setSmall">{{ t('projects.smallMode') }}</el-button>
      <el-button size="small" @mousedown.prevent @click="setOriginal">{{ t('projects.originalRatio') }}</el-button>
      <el-button size="small" type="danger" text @mousedown.prevent @click="remove">{{ t('projects.removeBlock') }}</el-button>
      <el-button size="small" :title="t('projects.moveUp')" :aria-label="t('projects.moveUp')" @mousedown.prevent @click="moveNode(-1)"><el-icon><ArrowUp /></el-icon></el-button>
      <el-button size="small" :title="t('projects.moveDown')" :aria-label="t('projects.moveDown')" @mousedown.prevent @click="moveNode(1)"><el-icon><ArrowDown /></el-icon></el-button>
      <label class="resource-width"><span>{{ t('projects.width') }}</span><input type="range" min="30" max="100" :value="width" @input="setWidth" /></label>
    </div>
    <div v-if="menuOpen" class="resource-context-menu" contenteditable="false">
      <button type="button" @mousedown.prevent @click="setSmall">{{ t('projects.smallMode') }}</button>
      <button type="button" @mousedown.prevent @click="setOriginal">{{ t('projects.originalRatio') }}</button>
      <button type="button" @mousedown.prevent @click="remove">{{ t('projects.removeBlock') }}</button>
      <button type="button" @mousedown.prevent @click="moveNode(-1)">{{ t('projects.moveUp') }}</button>
      <button type="button" @mousedown.prevent @click="moveNode(1)">{{ t('projects.moveDown') }}</button>
    </div>
  </NodeViewWrapper>
</template>

<style scoped>
.project-resource-node { position: relative; display: block; max-width: 100%; margin: 12px auto; outline: none; }
.project-resource-node.is-selected .project-resource-frame { outline: 2px solid var(--accent); outline-offset: 2px; }
.project-resource-frame { display: grid; min-height: 80px; margin: 0; overflow: hidden; justify-items: center; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-muted); }
.project-resource-frame img { display: block; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
.project-resource-frame figcaption { padding: 6px 10px; color: var(--muted); font-size: 10px; text-align: center; }
.resource-placeholder { align-self: center; padding: 18px; color: var(--muted); font-size: 12px; }
.resource-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; padding: 7px 1px; }
.resource-controls .el-button { margin: 0; }
.resource-width { display: flex; align-items: center; gap: 6px; margin-left: auto; color: var(--muted); font-size: 10px; }
.resource-width input { width: 82px; accent-color: var(--accent); }
.resource-context-menu { position: absolute; z-index: 4; top: 28px; right: 7px; display: grid; min-width: 128px; padding: 5px; border: 1px solid var(--line); border-radius: 7px; background: var(--surface); box-shadow: var(--shadow); }
.resource-context-menu button { padding: 8px; border: 0; background: transparent; color: var(--ink); text-align: left; }
@media (max-width: 768px) { .resource-controls { gap: 3px; } .resource-controls .el-button { font-size: 10px; } .resource-width { width: 100%; margin-left: 0; } .resource-width input { flex: 1; } }
</style>
