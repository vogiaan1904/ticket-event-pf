import { registerDecorator, ValidationOptions, ValidationArguments } from 'class-validator';

/**
 * Validates if a string is in ISO 8601 format (YYYY-MM-DD or YYYY-MM-DDThh:mm:ss.sssZ).
 * This matches the format used by Date.toISOString().
 */
export function IsISODateString(validationOptions?: ValidationOptions) {
  return function (object: Object, propertyName: string) {
    registerDecorator({
      name: 'isISODateString',
      target: object.constructor,
      propertyName: propertyName,
      options: validationOptions,
      validator: {
        validate(value: any) {
          if (typeof value !== 'string') {
            return false;
          }

          const isoDatePattern = /^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z?)?$/;

          if (!isoDatePattern.test(value)) {
            return false;
          }

          // The pattern admits impossible dates (2025-02-31); Date does not.
          const date = new Date(value);
          return !isNaN(date.getTime());
        },
        defaultMessage(args: ValidationArguments) {
          return `${args.property} must be a valid ISO date string (YYYY-MM-DD or YYYY-MM-DDThh:mm:ss.sssZ)`;
        },
      },
    });
  };
}
