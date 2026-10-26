# 在同一台机器顺序测试各场景，避免多个基准容器相互争抢 CPU 影响结论。
param([int]$Rounds = 10)
$ErrorActionPreference = 'Stop'
$toolRoot = $PSScriptRoot
$backendRoot = Split-Path (Split-Path $toolRoot -Parent) -Parent
$dataRoot = Join-Path $backendRoot 'dist/inspiration-benchmark'
$imageTag = 'photo-nini-inspiration-benchmark:20261026'
New-Item -ItemType Directory -Force $dataRoot | Out-Null

# 外部程序非零退出时主动终止，不能将下载失败、OOM 或无效模型记为通过。
function Invoke-CheckedDocker {
    param([string[]]$DockerArguments)
    & docker @DockerArguments
    if ($LASTEXITCODE -ne 0) { throw "Docker 基准命令失败，退出码 $LASTEXITCODE" }
}
Invoke-CheckedDocker -DockerArguments @('build', '-t', $imageTag, $toolRoot)
$dataMount = "type=bind,source=$dataRoot,target=/data"
Invoke-CheckedDocker -DockerArguments @('run', '--rm', '--entrypoint', 'python', '--mount', $dataMount,
    $imageTag, 'prepare.py', '--large')
foreach ($model in @('nano', 'tiny')) {
    foreach ($sessionMode in @('shared', 'per-worker')) {
        foreach ($imageSet in @('standard', 'large')) {
            $imagePath = if ($imageSet -eq 'large') { '/data/large' } else { '/data' }
            $resultName = "$model-$sessionMode-$imageSet.json"
            # 同时限制核配额和可用逻辑 CPU，关闭 swap，避免借用宿主额外核心或交换空间。
            Invoke-CheckedDocker -DockerArguments @('run', '--rm', '--cpus=2', '--cpuset-cpus=0,1',
                '--memory=2g', '--memory-swap=2g', '--network=none', '--mount', $dataMount,
                $imageTag, '--model', $model, '--sessions', $sessionMode, '--images', $imagePath,
                '--rounds', "$Rounds", '--output', "/data/$resultName")
        }
    }
}
# 汇总程序通过只读挂载执行，结果保留在 dist，人工审核后才收入 docs。
$scriptMount = "type=bind,source=$toolRoot,target=/scripts,readonly"
Invoke-CheckedDocker -DockerArguments @('run', '--rm', '--network=none', '--entrypoint', 'python',
    '--mount', $dataMount, '--mount', $scriptMount, $imageTag, '/scripts/summarize.py')
