param([string]$ReferencePath = "$PSScriptRoot/../docs/design/dark-theme-reference.png")

# The supplied design is the source. Only decorative artwork is cropped;
# text, inputs, options, scores and participant names remain real HTML.
Add-Type -AssemblyName System.Drawing
$source = [System.Drawing.Bitmap]::FromFile((Resolve-Path -LiteralPath $ReferencePath))
$assetDirectory = [System.IO.Path]::GetFullPath("$PSScriptRoot/../frontend/public/art")
[System.IO.Directory]::CreateDirectory($assetDirectory) | Out-Null
function Save-Crop($name, $x, $y, $width, $height, $ellipse = $false) {
    $bitmap = New-Object System.Drawing.Bitmap($width, $height)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    if ($ellipse) {
        $path = New-Object System.Drawing.Drawing2D.GraphicsPath
        $path.AddEllipse(0, 0, $width, $height)
        $graphics.SetClip($path)
    }
    $graphics.DrawImage($source, (New-Object System.Drawing.Rectangle(0, 0, $width, $height)), $x, $y, $width, $height, [System.Drawing.GraphicsUnit]::Pixel)
    $bitmap.Save("$assetDirectory/$name.png", [System.Drawing.Imaging.ImageFormat]::Png)
    $graphics.Dispose()
    $bitmap.Dispose()
}
# Book, card, feedback and trophy art come from docs/design/art via
# scripts/prepare-art.mjs rather than from this reference crop.
Save-Crop 'brand' 43 13 53 54
foreach ($avatar in @(@(0, 253), @(1, 286), @(2, 318), @(3, 350), @(4, 383), @(5, 415))) {
    Save-Crop "avatar-$($avatar[0])" 1425 $avatar[1] 25 25 $true
}
$source.Dispose()
