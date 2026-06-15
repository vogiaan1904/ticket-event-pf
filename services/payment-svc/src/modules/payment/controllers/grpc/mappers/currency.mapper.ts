import { Currency } from '@prisma/client';

export class CurrencyMapper {
  private static stringToPrismaMap = new Map<string, Currency>([['VND', Currency.VND]]);

  static toPrisma(string: string): Currency {
    const prismaCurrency = this.stringToPrismaMap.get(string);
    if (!prismaCurrency) {
      throw new Error(`Unknown string Currency: ${string}`);
    }
    return prismaCurrency;
  }
}
