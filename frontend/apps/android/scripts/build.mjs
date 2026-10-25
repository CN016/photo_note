import { existsSync, mkdirSync, readFileSync, writeFileSync, copyFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve, dirname, join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { syncWeb } from './sync.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const shellPackage = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'))
const web = resolve(root, '../web')
const win = process.platform === 'win32'
/** 子进程失败立即停止，防止 Web 构建失败后仍打包上次残留资源。 */
function run(command, args, cwd = root, env = process.env) {
  const result = spawnSync(command, args, { cwd, env, stdio: 'inherit', shell: win && /\.(cmd|bat)$/.test(command) })
  if (result.error || result.status !== 0) throw new Error(`${command} 执行失败：${result.error?.message || result.status}`)
}
try {
  if (Number(process.versions.node.split('.')[0]) < 22) throw new Error('需要 Node.js 22 或更高版本')
  const sdk = process.env.ANDROID_HOME || process.env.ANDROID_SDK_ROOT || (win ? join(process.env.LOCALAPPDATA || '', 'Android/Sdk') : '')
  if (!existsSync(join(sdk, 'platforms/android-37.2/android.jar'))) throw new Error('缺少 Android SDK 37.2，请设置 ANDROID_HOME 并安装 platforms;android-37.2')
  const studioJdk = 'C:/Program Files/Android/Android Studio/jbr'
  const javaHome = process.env.PHOTO_NINI_JAVA_HOME || (win && existsSync(join(studioJdk, 'bin/java.exe')) ? studioJdk : process.env.JAVA_HOME)
  if (!javaHome) throw new Error('请设置 PHOTO_NINI_JAVA_HOME，指向 JDK 21')
  const version = spawnSync(join(javaHome, 'bin', win ? 'java.exe' : 'java'), ['-version'], { encoding: 'utf8' })
  if (!/version "21\./.test(version.stderr || '')) throw new Error('本工程锁定 JDK 21，请设置 PHOTO_NINI_JAVA_HOME')
  if (!existsSync(join(web, 'node_modules'))) throw new Error('请先在 frontend/apps/web 执行 pnpm install --frozen-lockfile')
  // 复用生产地址但不输出完整配置；发布包不能通过环境变量误接明文服务。
  const envText = existsSync(join(web, '.env')) ? readFileSync(join(web, '.env'), 'utf8') : ''
  const productionText = existsSync(join(web, '.env.production')) ? readFileSync(join(web, '.env.production'), 'utf8') : ''
  const configured = process.env.VITE_API_BASE_URL || [...(envText + '\n' + productionText).matchAll(/^VITE_API_BASE_URL\s*=\s*(.+)$/gm)].at(-1)?.[1]?.trim().replace(/^["']|["']$/g, '')
  if (!configured || new URL(configured).protocol !== 'https:') throw new Error('安卓包要求 Web 生产配置中 VITE_API_BASE_URL 为 HTTPS 地址')
  const env = { ...process.env, JAVA_HOME: javaHome, ANDROID_HOME: sdk, VITE_API_BASE_URL: configured, PHOTO_NINI_ANDROID_BUILD: '1' }
  run(win ? 'pnpm.cmd' : 'pnpm', ['build'], web, env)
  syncWeb(configured)
  const release = process.argv.includes('--release')
  if (release && ['PHOTO_NINI_KEYSTORE', 'PHOTO_NINI_STORE_PASSWORD', 'PHOTO_NINI_KEY_ALIAS', 'PHOTO_NINI_KEY_PASSWORD'].some(key => !process.env[key])) throw new Error('正式构建需要完整的外部签名配置：PHOTO_NINI_KEYSTORE、PHOTO_NINI_STORE_PASSWORD、PHOTO_NINI_KEY_ALIAS、PHOTO_NINI_KEY_PASSWORD')
  run(win ? 'gradlew.bat' : './gradlew', [release ? 'assembleRelease' : 'assembleDebug', 'lintDebug', '--console=plain'], join(root, 'native'), env)
  const type = release ? 'release' : 'debug'
  for (const abi of ['arm64-v8a', 'armeabi-v7a', 'x86_64']) {
    const apk = join(root, `native/app/build/outputs/apk/${type}/app-${abi}-${type}.apk`)
    const output = join(root, 'artifacts', `photo-nini-${shellPackage.version}-${abi}-${type}.apk`)
    mkdirSync(dirname(output), { recursive: true })
    copyFileSync(apk, output)
    const sha = createHash('sha256').update(readFileSync(output)).digest('hex')
    writeFileSync(`${output}.sha256`, `${sha}  ${output.split(/[\\/]/).at(-1)}\n`)
    console.log(`APK: ${output}\nSHA-256: ${sha}`)
  }
} catch (error) { console.error(error.message); process.exitCode = 1 }
