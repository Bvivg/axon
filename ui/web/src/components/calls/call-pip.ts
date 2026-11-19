type WebKitVideo = HTMLVideoElement & {
  webkitPresentationMode?: string;
  webkitSupportsPresentationMode?: (mode: string) => boolean;
  webkitSetPresentationMode?: (mode: string) => void;
};

export function supportsCallPiP(video: HTMLVideoElement) {
  const webkit = video as WebKitVideo;
  return (document.pictureInPictureEnabled && typeof video.requestPictureInPicture === "function")
    || webkit.webkitSupportsPresentationMode?.("picture-in-picture") === true;
}

export function isCallPiP(video: HTMLVideoElement) {
  return document.pictureInPictureElement === video || (video as WebKitVideo).webkitPresentationMode === "picture-in-picture";
}

export async function openCallPiP(video: HTMLVideoElement) {
  if (document.pictureInPictureEnabled && typeof video.requestPictureInPicture === "function") {
    await video.requestPictureInPicture();
  } else {
    (video as WebKitVideo).webkitSetPresentationMode?.("picture-in-picture");
  }
}

export async function closeCallPiP(video: HTMLVideoElement) {
  if (document.pictureInPictureElement === video) await document.exitPictureInPicture();
  else if (isCallPiP(video)) (video as WebKitVideo).webkitSetPresentationMode?.("inline");
}
