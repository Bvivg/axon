"use client";

import { Smile } from "lucide-react";
import type { RefObject } from "react";

import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";

const emojis = [
  ["😀", "Grinning face"], ["😃", "Happy face"], ["😂", "Tears of joy"], ["🥰", "Smiling with hearts"], ["😍", "Heart eyes"], ["😎", "Sunglasses"],
  ["😊", "Smiling face"], ["😉", "Winking face"], ["🤔", "Thinking face"], ["😅", "Smiling with sweat"], ["😭", "Crying face"], ["😮", "Surprised face"],
  ["👍", "Thumbs up"], ["👎", "Thumbs down"], ["👏", "Clapping hands"], ["🙌", "Raised hands"], ["🤝", "Handshake"], ["👋", "Waving hand"],
  ["❤️", "Red heart"], ["💜", "Purple heart"], ["🔥", "Fire"], ["✨", "Sparkles"], ["🎉", "Party popper"], ["🎮", "Game controller"],
] as const;

export function EmojiPicker({ onPick, returnFocus }: { onPick: (emoji: string) => void; returnFocus: RefObject<HTMLTextAreaElement | null> }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon" className="chat-composer-emoji" aria-label="Choose emoji"><Smile aria-hidden /></Button>} />
      <DropdownMenuContent aria-label="Emoji" side="top" align="end" finalFocus={returnFocus} className="grid w-64 max-w-[calc(100vw-24px)] grid-cols-6 gap-1 rounded-2xl p-2">
        {emojis.map(([emoji, label]) => <DropdownMenuItem key={label} aria-label={label} className="min-h-9 justify-center px-0 text-xl" onClick={() => onPick(emoji)}>{emoji}</DropdownMenuItem>)}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
