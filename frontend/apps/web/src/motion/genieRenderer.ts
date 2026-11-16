import { geniePoint, type GenieRect } from './genieGeometry'

/** 私有图片只上传当前导航的临时 GPU 纹理，不读回、不导出、不传输；退出时主动释放。 */
export interface GenieRenderer { draw(progress: number): void; update(source: HTMLCanvasElement): void; dispose(): void }

/** 用连续共享顶点网格绘制 Genie 曲面，避免逐条 DOM 切片造成接缝和大量布局计算。 */
export function createGenieRenderer(textureSource: HTMLCanvasElement, destination: GenieRect, onFailure: () => void): GenieRenderer | null {
  const canvas = document.createElement('canvas')
  const width = window.innerWidth, height = window.innerHeight
  const ratio = Math.min(window.devicePixelRatio || 1, 1.5, 1600 / Math.max(width, height))
  canvas.width = Math.max(1, Math.round(width * ratio)); canvas.height = Math.max(1, Math.round(height * ratio))
  canvas.dataset.genieMotion = ''
  canvas.setAttribute('aria-hidden', 'true')
  Object.assign(canvas.style, { position: 'fixed', inset: '0', width: '100vw', height: '100dvh', zIndex: '81', pointerEvents: 'none' })
  let gl: WebGLRenderingContext | null = null
  try { gl = canvas.getContext('webgl', { alpha: true, antialias: true, premultipliedAlpha: false, preserveDrawingBuffer: false }) } catch { return null }
  if (!gl) return null
  const context = gl
  const shaders: WebGLShader[] = []
  const program = context.createProgram(), buffer = context.createBuffer(), texture = context.createTexture()
  let disposed = false
  const dispose = () => {
    if (disposed) return
    disposed = true
    canvas.removeEventListener('webglcontextlost', lost)
    canvas.remove(); context.deleteTexture(texture); context.deleteBuffer(buffer)
    if (program) context.deleteProgram(program)
    for (const shader of shaders) context.deleteShader(shader)
    context.getExtension('WEBGL_lose_context')?.loseContext()
    canvas.width = 0; canvas.height = 0
  }
  // 上下文丢失交由调用端恢复 DOM，不能把用户留在透明页面。
  const lost = (event: Event) => { event.preventDefault(); onFailure(); dispose() }
  try {
    if (!program || !buffer || !texture) { dispose(); return null }
    const compile = (type: number, source: string) => {
      const shader = context.createShader(type)
      if (!shader) throw new Error('shader')
      shaders.push(shader); context.shaderSource(shader, source); context.compileShader(shader)
      if (!context.getShaderParameter(shader, context.COMPILE_STATUS)) throw new Error('compile')
      context.attachShader(program, shader)
    }
    compile(context.VERTEX_SHADER, 'attribute vec2 a_position; attribute vec2 a_uv; varying vec2 v_uv; void main(){ gl_Position=vec4(a_position,0.,1.); v_uv=a_uv; }')
    compile(context.FRAGMENT_SHADER, 'precision mediump float; uniform sampler2D u_image; uniform float u_opacity; varying vec2 v_uv; void main(){ vec4 color=texture2D(u_image,v_uv); gl_FragColor=vec4(color.rgb,color.a*u_opacity); }')
    context.linkProgram(program)
    if (!context.getProgramParameter(program, context.LINK_STATUS)) throw new Error('link')
    context.useProgram(program); context.bindBuffer(context.ARRAY_BUFFER, buffer)
    for (const [name, offset] of [['a_position', 0], ['a_uv', 8]] as const) {
      const location = context.getAttribLocation(program, name)
      context.enableVertexAttribArray(location); context.vertexAttribPointer(location, 2, context.FLOAT, false, 16, offset)
    }
    context.activeTexture(context.TEXTURE0); context.bindTexture(context.TEXTURE_2D, texture)
    context.texParameteri(context.TEXTURE_2D, context.TEXTURE_MIN_FILTER, context.LINEAR)
    context.texParameteri(context.TEXTURE_2D, context.TEXTURE_MAG_FILTER, context.LINEAR)
    context.texParameteri(context.TEXTURE_2D, context.TEXTURE_WRAP_S, context.CLAMP_TO_EDGE)
    context.texParameteri(context.TEXTURE_2D, context.TEXTURE_WRAP_T, context.CLAMP_TO_EDGE)
    context.texImage2D(context.TEXTURE_2D, 0, context.RGBA, context.RGBA, context.UNSIGNED_BYTE, textureSource)
    context.uniform1i(context.getUniformLocation(program, 'u_image'), 0)
    const opacity = context.getUniformLocation(program, 'u_opacity')
    // 网格无重叠面，直接写出非预乘颜色与 alpha，交由浏览器合成，避免交接时重复乘 alpha 变黑。
    context.viewport(0, 0, canvas.width, canvas.height)
    // 每行 8 个横向区间保持纹理插值平滑，纵向 96 行逼近两条贝塞尔曲线；单次 drawCall。
    const columns = 8, rows = 96, vertices = new Float32Array(columns * rows * 6 * 4)
    const source = { x: 0, y: 0, width, height }
    const corners = [[0, 0], [0, 1], [1, 0], [1, 0], [0, 1], [1, 1]] as const
    const draw = (progress: number) => {
      if (disposed) return
      if (window.innerWidth !== width || window.innerHeight !== height) { onFailure(); dispose(); return }
      let offset = 0
      for (let row = 0; row < rows; row++) {
        const top = row / rows, bottom = (row + 1) / rows
        const leftTop = geniePoint(source, destination, progress, 0, top), rightTop = geniePoint(source, destination, progress, 1, top)
        const leftBottom = geniePoint(source, destination, progress, 0, bottom), rightBottom = geniePoint(source, destination, progress, 1, bottom)
        for (let col = 0; col < columns; col++) {
          for (const [horizontal, vertical] of corners) {
            const u = (col + horizontal) / columns, v = vertical ? bottom : top
            const left = vertical ? leftBottom : leftTop, right = vertical ? rightBottom : rightTop
            vertices[offset++] = (left[0] + (right[0] - left[0]) * u) / width * 2 - 1
            vertices[offset++] = 1 - left[1] / height * 2
            vertices[offset++] = u; vertices[offset++] = v
          }
        }
      }
      // 最后 12% 在原卡片处交接，避免将带留白的详情快照硬切到列表裁图。
      const blend = Math.max(0, Math.min(1, progress / 0.12))
      context.uniform1f(opacity, blend * blend * (3 - 2 * blend))
      context.clearColor(0, 0, 0, 0); context.clear(context.COLOR_BUFFER_BIT)
      context.bufferData(context.ARRAY_BUFFER, vertices, context.DYNAMIC_DRAW)
      context.drawArrays(context.TRIANGLES, 0, vertices.length / 4)
    }
    canvas.addEventListener('webglcontextlost', lost)
    document.body.appendChild(canvas)
    return { draw, dispose, update(source) {
      if (!disposed) context.texImage2D(context.TEXTURE_2D, 0, context.RGBA, context.RGBA, context.UNSIGNED_BYTE, source)
    } }
  } catch { dispose(); return null }
}
