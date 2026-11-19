export const maxMessageLength = 65_536;

export function messageTooLong(body: string): boolean {
  return Array.from(body.trim()).length > maxMessageLength;
}

export const messageLengthError = `A message can contain at most ${maxMessageLength} characters.`;
