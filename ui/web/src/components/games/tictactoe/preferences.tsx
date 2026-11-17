"use client";

import { ChartNoAxesColumn } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { ArenaDialog, actionClass, iconClass } from "@/components/games/tictactoe/arena";
import { type LocalMark, type LocalMode } from "@/lib/games/tictactoe";
import { setTheme, Theme, useTheme } from "@/lib/theme";

export type GamePreferences = { mode: LocalMode; duration: number; side: LocalMark };
export const defaultPreferences: GamePreferences = { mode: "local", duration: 60000, side: "X" };
export const difficultyLabels = { easy: "Лёгкий", medium: "Средний", hard: "Сильный", local: "" };
const preferencesKey = "axon-ttt-settings-v1";

export function readPreferences(): GamePreferences {
  try {
    const value = JSON.parse(localStorage.getItem(preferencesKey) ?? "null");
    return { mode: ["easy", "medium", "hard", "local"].includes(value?.mode) ? value.mode : defaultPreferences.mode,
      duration: [30000, 60000, 180000, 300000].includes(value?.duration) ? value.duration : defaultPreferences.duration,
      side: value?.side === "O" ? "O" : "X" };
  } catch { return defaultPreferences; }
}

export function storePreferences(value: GamePreferences) {
  try { localStorage.setItem(preferencesKey, JSON.stringify(value)); } catch {}
}

function Segments<T extends string | number>({ label, value, options, change, disabled }: { label: string; value: T; options: [T, string][]; change: (value: T) => void; disabled?: boolean }) {
  return <fieldset disabled={disabled} className="grid min-w-0 gap-1.5 disabled:opacity-60"><legend className="mb-1.5 text-[13px] text-game-text2">{label}</legend><div className="flex gap-0.5 rounded-game-sm bg-game-bg2 p-[3px]">{options.map(([id, text]) => <label key={id} className="relative flex min-h-[44px] min-w-0 flex-1 cursor-pointer items-center justify-center rounded-[7px] px-1 text-center text-[13px] text-game-text2 has-checked:bg-game-card has-checked:text-game-text has-checked:shadow-[0_0_0_1px_var(--color-game-border),0_1px_2px_#00000014] has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-game-primary"><input className="absolute inset-0 size-full cursor-pointer opacity-0" type="radio" name={label} value={id} checked={value === id} onChange={() => change(id)} />{text}</label>)}</div></fieldset>;
}

export function GameSettings({ open, onOpenChange, preferences, change, online = false, children }: { open: boolean; onOpenChange: (open: boolean) => void; preferences: GamePreferences; change?: (value: GamePreferences) => void; online?: boolean; children?: ReactNode }) {
  const theme = useTheme();
  return <ArenaDialog title="Настройки" open={open} onOpenChange={onOpenChange}>
    <Segments label="Тема" value={theme} options={[[Theme.System, "Системная"], [Theme.Light, "Светлая"], [Theme.Dark, "Тёмная"]]} change={setTheme} />
    <Segments label="Соперник" value={preferences.mode === "local" ? "local" : "ai"} options={[["local", online ? "Игрок онлайн" : "Игрок"], ["ai", "Компьютер"]]} disabled={online} change={value => change?.({ ...preferences, mode: value === "local" ? "local" : "hard" })} />
    <Segments label="Время на каждого игрока" value={preferences.duration} options={[[30000, "30 с"], [60000, "1 мин"], [180000, "3 мин"], [300000, "5 мин"]]} disabled={online} change={duration => change?.({ ...preferences, duration })} />
    {!online && preferences.mode !== "local" ? <><Segments label="Сложность" value={preferences.mode} options={[["easy", "Лёгкая"], ["medium", "Средняя"], ["hard", "Сильная"]]} change={mode => change?.({ ...preferences, mode })} /><Segments label="Вы играете за" value={preferences.side} options={[["X", "Крестики"], ["O", "Нолики"]]} change={side => change?.({ ...preferences, side })} /></> : null}
    {online ? <p className="text-[13px] text-game-text2">Параметры сетевой партии и часы определяет сервер.</p> : null}{children}
  </ArenaDialog>;
}

type Stats = { X: number; O: number; draw: number };
const emptyStats: Stats = { X: 0, O: 0, draw: 0 };

export function GameStatistics({ open, onOpenChange, winner, gameKey, storageKey = "axon-ttt-stats-v1" }: { open: boolean; onOpenChange: (open: boolean) => void; winner?: LocalMark | "draw"; gameKey: string | number; storageKey?: string }) {
  const [stats, setStats] = useState(emptyStats);
  const counted = useRef<string | number | null>(null);
  useEffect(() => {
    let next = { ...emptyStats };
    let games: (string | number)[] = [];
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey) ?? "null");
      for (const key of ["X", "O", "draw"] as const) if (Number.isSafeInteger(saved?.[key]) && saved[key] >= 0) next[key] = saved[key];
      if (Array.isArray(saved?.games)) games = saved.games;
    } catch {}
    if (winner && counted.current !== gameKey && !games.includes(gameKey)) {
      next = { ...next, [winner]: next[winner] + 1 };
      counted.current = gameKey;
      games.push(gameKey);
      try { localStorage.setItem(storageKey, JSON.stringify({ ...next, games: games.slice(-500) })); } catch {}
    }
    setStats(next);
  }, [winner, gameKey, storageKey]);
  return <ArenaDialog title="Статистика" open={open} onOpenChange={onOpenChange} footer={<><button type="button" className={`${actionClass} text-game-error`} onClick={() => { setStats(emptyStats); try { localStorage.setItem(storageKey, JSON.stringify({ ...emptyStats, games: [gameKey] })); } catch {} }}>Сбросить</button><button type="button" className={actionClass} onClick={() => onOpenChange(false)}>Закрыть</button></>}>
    <div className="grid grid-cols-3 gap-2">{([['X', 'Крестики'], ['draw', 'Ничьи'], ['O', 'Нолики']] as const).map(([key, label]) => <div key={key} className="grid justify-items-center gap-1.5 rounded-game-md border border-game-border bg-game-bg px-2 pt-3.5 pb-3"><b className="font-mono text-[26px] leading-none font-medium">{stats[key]}</b><span className="text-[13px] text-game-text2">{label}</span></div>)}</div><div className="flex justify-between text-game-text2"><span>Партий сыграно</span><span className="font-mono">{stats.X + stats.O + stats.draw}</span></div>
  </ArenaDialog>;
}

export function StatisticsIcon({ onClick }: { onClick: () => void }) {
  return <button type="button" aria-label="Статистика" title="Статистика" className={iconClass} onClick={onClick}><ChartNoAxesColumn aria-hidden /></button>;
}
