async function asPng(url: string): Promise<Blob> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`The image did not load (${response.status}).`);
  const blob = await response.blob();
  if (blob.type === "image/png") return blob;

  const bitmap = await createImageBitmap(blob);
  try {
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("The image could not be converted.");
    context.drawImage(bitmap, 0, 0);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob(
        (png) => (png ? resolve(png) : reject(new Error("The image could not be converted."))),
        "image/png",
      ),
    );
  } finally {
    bitmap.close();
  }
}

export async function copyImage(url: string): Promise<void> {
  if (typeof ClipboardItem === "undefined" || !navigator.clipboard?.write) {
    throw new Error("This browser cannot copy images.");
  }
  await navigator.clipboard.write([new ClipboardItem({ "image/png": asPng(url) })]);
}
