import { Pencil, Sun, Moon, SunMoon, Inbox, CalendarDays, CircleAlert, ArrowRight, Clock3, ListTodo, Check, Folder, Plus, FileText, BookOpen, Plug, KeyRound, Hash, Search, RefreshCw, LockKeyhole, Sparkle, Menu, X, LayoutDashboard, ChevronRight, ArrowLeft, Trash2, Link2, FolderTree, SquareKanban, User, Users, LockKeyholeOpen, Bell, Eye, EyeOff, Zap, Play, FolderInput, type LucideIcon } from "lucide-react";
const icons = { edit: Pencil, sun: Sun, moon: Moon, theme: SunMoon, inbox: Inbox, calendar: CalendarDays, alert: CircleAlert, arrow: ArrowRight, clock: Clock3, list: ListTodo, check: Check, folder: Folder, plus: Plus, file: FileText, book: BookOpen, plug: Plug, key: KeyRound, hash: Hash, search: Search, refresh: RefreshCw, lock: LockKeyhole, spark: Sparkle, menu: Menu, close: X, dashboard: LayoutDashboard, chevron: ChevronRight, back: ArrowLeft, trash: Trash2, link: Link2, tree: FolderTree, board: SquareKanban, user: User, users: Users, unlock: LockKeyholeOpen, bell: Bell, eye: Eye, eyeOff: EyeOff, zap: Zap, play: Play, move: FolderInput } satisfies Record<string, LucideIcon>;
export type IconName = keyof typeof icons;
export default function Icon({ name, size = 18, className = "" }: { name: IconName; size?: number; className?: string }) {
  const Glyph = icons[name];
  return <Glyph className={`ui-icon ${className}`} size={size} strokeWidth={2} aria-hidden="true" />;
}
