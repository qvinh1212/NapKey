export const ecosystemClients = [
  { id: 'claudeCode', badge: 'CLI', mark: 'C' },
  { id: 'cursor', badge: 'IDE', mark: 'Cu' },
  { id: 'vscode', badge: 'IDE', mark: '<>' },
  { id: 'cline', badge: 'EXT', mark: 'Cl' },
  { id: 'openCode', badge: 'CLI', mark: 'O' },
  { id: 'sdk', badge: 'SDK', mark: '{}' },
] as const;

export const ecosystemModels = [
  { id: 'claude-opus-4.8', family: 'opus', mark: 'O' },
  { id: 'claude-sonnet-4.6', family: 'sonnet', mark: 'S' },
  { id: 'claude-haiku-4.5', family: 'haiku', mark: 'H' },
  { id: 'auto', family: 'router', mark: 'A' },
] as const;

const inboundRows = [116, 194, 272, 350, 428, 506] as const;
const outboundRows = [155, 255, 365, 465] as const;

export const ecosystemInboundSignals = inboundRows.map((y, index) => ({
  path: `M 350 ${y} C 470 ${y}, 480 310, 600 310`,
  duration: 2.8 + (index % 3) * 0.25,
  delay: index * 0.52,
}));

export const ecosystemOutboundSignals = outboundRows.map((y, index) => ({
  path: `M 600 310 C 730 310, 735 ${y}, 850 ${y}`,
  duration: 2.5 + (index % 2) * 0.3,
  delay: 0.38 + index * 0.74,
}));
