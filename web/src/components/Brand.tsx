import Icon from "./Icon";

export default function Brand() {
  return <span className="brand"><span className="brand-mark"><Icon name="spark" size={16} /></span>Lumen <small>/ studio</small></span>;
}

export function Signature() {
  return <span className="signature"><svg width="16" height="14" viewBox="0 0 120 100" fill="currentColor" aria-hidden="true"><path d="M6 22 31 88H45L79 9H61L37 67 23 22Z" /><path d="M88 10C104 9 115 18 116 30H100C99 25 94 23 87 25ZM83 39C96 43 116 46 116 64C116 83 98 94 76 88L64 82 71 67C82 76 99 75 99 66C99 60 88 59 76 54Z" /></svg>by velessoft</span>;
}
