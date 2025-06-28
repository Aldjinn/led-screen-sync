# Get version from git tag or use "dev"
try {
    $VERSION = git describe --tags --exact-match 2>$null
    if (-not $VERSION) {
        $VERSION = "dev-$(git rev-parse --short HEAD)"
    }
} catch {
    $VERSION = "dev"
}

$COMMIT = git rev-parse HEAD
$DATE = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"

Write-Host "Building version: $VERSION"
Write-Host "Commit: $COMMIT"
Write-Host "Date: $DATE"

go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" -o led-screen-sync.exe

.\led-screen-sync.exe -v