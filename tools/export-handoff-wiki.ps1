param(
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [string]$SourceRevision = 'main'
)

# Export only; never publish, push, or overwrite an existing wiki checkout.
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$exportRoot = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $exportRoot) {
    if ((Get-ChildItem -LiteralPath $exportRoot -Force | Measure-Object).Count -ne 0) {
        throw 'Choose an empty output directory; this exporter does not overwrite files.'
    }
}
$pages = @(Get-ChildItem -LiteralPath (Join-Path $repoRoot 'docs/handoff') -Filter '*.md' -Recurse)
$pages += @(Get-ChildItem -LiteralPath (Join-Path $repoRoot 'docs/reviews') -Filter '2026-09-18-*.md')
$mapping = @{}
foreach ($page in $pages) {
    $rel = [IO.Path]::GetRelativePath($repoRoot, $page.FullName).Replace('\', '/')
    if ($rel -eq 'docs/handoff/README.md') { $name = 'Home' }
    elseif ($rel.StartsWith('docs/handoff/')) { $name = $rel.Substring(13).Replace('/', '-').Replace('.md', '') }
    else { $name = 'review-' + $page.BaseName }
    if ($mapping.Values -contains $name) { throw "Duplicate wiki page name: $name" }
    $mapping[$page.FullName] = $name
}
New-Item -ItemType Directory -Path $exportRoot -Force | Out-Null
foreach ($page in $pages) {
    $body = Get-Content -LiteralPath $page.FullName -Raw
    $rewritten = [regex]::Replace($body, '(?<prefix>!?\[[^\]]*\]\()(?<target>[^)]+)\)', {
        param($match)
        $target = $match.Groups['target'].Value.Trim('<', '>')
        if ($target -match '^(?:[a-z]+:|#|/)') { return $match.Value }
        $parts = $target.Split('#', 2)
        $absolute = [IO.Path]::GetFullPath((Join-Path $page.DirectoryName $parts[0]))
        $fragment = if ($parts.Count -eq 2) { '#' + $parts[1] } else { '' }
        if ($mapping.ContainsKey($absolute)) {
            return $match.Groups['prefix'].Value + $mapping[$absolute] + $fragment + ')'
        }
        if (-not $absolute.StartsWith($repoRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Link escapes repository: $target"
        }
        if (-not (Test-Path -LiteralPath $absolute)) { throw "Missing link target: $target" }
        $relative = [IO.Path]::GetRelativePath($repoRoot, $absolute).Replace('\', '/')
        $encoded = ($relative.Split('/') | ForEach-Object { [Uri]::EscapeDataString($_) }) -join '/'
        $kind = if (Test-Path -LiteralPath $absolute -PathType Container) { 'tree' } else { 'blob' }
        $url = 'https://github.com/mockagents/mockagents/' + $kind + '/' + [Uri]::EscapeDataString($SourceRevision) + '/' + $encoded + $fragment
        return $match.Groups['prefix'].Value + $url + ')'
    })
    [IO.File]::WriteAllText((Join-Path $exportRoot ($mapping[$page.FullName] + '.md')), $rewritten, [Text.UTF8Encoding]::new($false))
}
$sidebar = "# MockAgents handoff`n`n[Home](Home)`n`n"
foreach ($name in ($mapping.Values | Sort-Object)) {
    if ($name -ne 'Home') { $sidebar += '- [' + $name + '](' + $name + ")`n" }
}
[IO.File]::WriteAllText((Join-Path $exportRoot '_Sidebar.md'), $sidebar, [Text.UTF8Encoding]::new($false))
Write-Output "Exported $($pages.Count) pages and sidebar to $exportRoot. Nothing was published."
