const recompressAboveBytes = 1.5 * 1024 * 1024;
const maxSide = 2560;
const quality = 0.9;

const isHeic = (file: File) =>
  /^image\/hei[cf]$/i.test(file.type) || /\.(heic|heif)$/i.test(file.name);

const renamed = (name: string, extension: string) => `${name.replace(/\.[^./]+$/, "") || "photo"}${extension}`;

function encode(canvas: HTMLCanvasElement, type: string): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, type, quality));
}

export async function prepareImage(file: File): Promise<File> {
  if (isHeic(file)) {
    const { default: heic2any } = await import("heic2any");
    const converted = await heic2any({ blob: file, toType: "image/jpeg", quality });
    const blob = Array.isArray(converted) ? converted[0] : converted;
    return new File([blob], renamed(file.name, ".jpg"), { type: "image/jpeg" });
  }

  if (!/^image\/(jpeg|png|webp)$/.test(file.type) || file.size <= recompressAboveBytes) {
    return file;
  }

  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    return file;
  }
  try {
    const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(bitmap.width * scale);
    canvas.height = Math.round(bitmap.height * scale);
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);

    let blob = await encode(canvas, "image/webp");
    if (blob?.type !== "image/webp") {
      if (file.type === "image/png") return file;
      blob = await encode(canvas, "image/jpeg");
    }
    if (!blob || blob.size >= file.size) return file;
    return new File([blob], renamed(file.name, blob.type === "image/webp" ? ".webp" : ".jpg"), { type: blob.type });
  } finally {
    bitmap.close();
  }
}
