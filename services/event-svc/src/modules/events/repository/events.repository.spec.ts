import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { PrismaClient } from '@prisma/client';
import { execSync } from 'child_process';
import { randomUUID } from 'crypto';
import { CreateEventDto } from '../dtos';
import { EventsRepository } from './events.repository';

// Against a real Postgres: EVENT_TEST_DATABASE_URL, migrated here. Without it the suite
// skips locally and fails in CI, where a suite that skips itself asserts nothing.
const url = process.env.EVENT_TEST_DATABASE_URL;
if (!url && process.env.CI) {
  throw new Error('EVENT_TEST_DATABASE_URL is not set: the repository suite would assert nothing');
}
const suite = url ? describe : describe.skip;

const CAT1 = '00000000-0000-4000-8000-0000000000e1';
const CAT2 = '00000000-0000-4000-8000-0000000000e2';

const event = (): CreateEventDto => ({
  name: `Show ${randomUUID()}`,
  description: 'd',
  startDate: new Date('2027-01-01T10:00:00Z'),
  endDate: new Date('2027-01-02T10:00:00Z'),
  thumbnailUrl: 'https://example.com/t.png',
  venue: 'Hall',
  street: '1 St',
  ward: 'Ward One',
  district: 'District One',
  city: 'HCMC',
  country: 'VN',
  categoryIds: [CAT1],
  organizerName: 'Org',
  organizerDescription: 'od',
  organizerLogoUrl: 'https://example.com/o.png',
  createdBy: randomUUID(),
});

suite('EventsRepository against Postgres', () => {
  let prisma: PrismaClient;
  let repo: EventsRepository;

  beforeAll(async () => {
    execSync('npx prisma migrate deploy', {
      env: { ...process.env, DATABASE_URL: url },
      stdio: 'ignore',
    });
    prisma = new PrismaClient({ datasources: { db: { url } } });
    repo = new EventsRepository(prisma as PrismaService);
    for (const id of [CAT1, CAT2]) {
      await prisma.category.upsert({ where: { id }, update: {}, create: { id, name: id } });
    }
  }, 60_000);

  afterAll(async () => prisma?.$disconnect());

  it('an update replaces the categories with those it names', async () => {
    const created = await repo.create(event());
    await repo.update(created.id, { categoryIds: [CAT1, CAT2] });
    const read = await repo.findById(created.id);
    expect(read?.categories.map((c) => c.id).sort()).toEqual([CAT1, CAT2]);
  });

  it('an update that names no categories keeps them', async () => {
    const created = await repo.create(event());
    await repo.update(created.id, { name: 'renamed', categoryIds: [] });
    const read = await repo.findById(created.id);
    expect(read?.categories.map((c) => c.id)).toEqual([CAT1]);
  });

  it('an update that leaves out the ward and district keeps them', async () => {
    const created = await repo.create(event());
    await repo.update(created.id, { name: 'renamed' });
    const read = await repo.findById(created.id);
    expect(read?.location).toMatchObject({ ward: 'Ward One', district: 'District One' });
  });

  it('an update reads back every field it sets', async () => {
    const created = await repo.create(event());
    await repo.update(created.id, {
      name: 'Two',
      description: 'd2',
      startDate: new Date('2027-02-01T19:00:00Z'),
      endDate: new Date('2027-02-01T22:00:00Z'),
      thumbnailUrl: 'https://example.com/t2.png',
      venue: 'Hall Two',
      street: '2 St',
      ward: 'Ward Two',
      district: 'District Two',
      city: 'Hanoi',
      country: 'Vietnam',
      organizerName: 'Org Two',
      organizerDescription: 'od2',
      organizerLogoUrl: 'https://example.com/o2.png',
    });
    const read = await repo.findById(created.id);
    expect(read).toMatchObject({
      name: 'Two',
      description: 'd2',
      startDate: new Date('2027-02-01T19:00:00Z'),
      endDate: new Date('2027-02-01T22:00:00Z'),
      thumbnailUrl: 'https://example.com/t2.png',
      location: {
        venue: 'Hall Two',
        street: '2 St',
        ward: 'Ward Two',
        district: 'District Two',
        city: 'Hanoi',
        country: 'Vietnam',
      },
      organizer: { name: 'Org Two', description: 'od2', logoUrl: 'https://example.com/o2.png' },
    });
  });
});
