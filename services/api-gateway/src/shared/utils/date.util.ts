export function parseDdMmYyyyToDate(dateStr: string): Date {
  if (typeof dateStr !== 'string') return new Date('');
  const match = /^([0-9]{2})-([0-9]{2})-([0-9]{4})$/.exec(dateStr.trim());
  if (!match) return new Date('');
  const day = parseInt(match[1], 10);
  const month = parseInt(match[2], 10);
  const year = parseInt(match[3], 10);
  // Construct in UTC to avoid TZ shifts
  const d = new Date(Date.UTC(year, month - 1, day, 0, 0, 0, 0));
  return d;
}
