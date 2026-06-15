import { PrismaClient, EventStatus } from '@prisma/client';
import axios from 'axios';

const prisma = new PrismaClient();

// TicketBox API Configuration
const TICKETBOX_SEARCH_API = 'https://api-v2.ticketbox.vn/search/v2/events';
const TICKETBOX_DETAIL_API = 'https://api-v2.ticketbox.vn/gin/api/v1/events';
const MUSIC_CATEGORY_ID = '537b9143-5e62-4bd4-a992-974a8cb217d5';
const EVENTS_PER_PAGE = 20;
const TOTAL_PAGES = 2; // 40 events total

// Event statuses for random assignment
const EVENT_STATUSES: EventStatus[] = ['DRAFT', 'CONFIGURED', 'APPROVED', 'PUBLISHED', 'CANCELLED'];

interface TicketBoxEvent {
  id: number;
  name: string;
  imageUrl: string;
  orgLogoUrl: string;
  day: string;
  price: number;
  deeplink: string;
  isNewBookingFlow: boolean;
  originalId: number;
  url: string;
  badge: string | null;
  categories: string[];
}

interface TicketBoxEventDetail {
  id: number;
  title: string;
  bannerURL: string;
  url: string;
  description: string;
  type: number;
  venue: string;
  address: string;
  orgLogoURL: string;
  orgName: string;
  orgDescription: string;
  isFree: boolean;
  minTicketPrice: number;
  startTime: string;
  endTime: string;
}

interface TicketBoxResponse {
  code: number;
  message: string;
  data: {
    results: TicketBoxEvent[];
    pagination: {
      hasMore: boolean;
      pageSize: number;
      currentPage: number;
    };
  };
}

interface TicketBoxDetailResponse {
  status: number;
  message: string;
  data: {
    result: TicketBoxEventDetail;
  };
}

interface ParsedAddress {
  street: string;
  ward: string | null;
  district: string | null;
  city: string;
  country: string;
}

/**
 * Get random event status
 */
function getRandomEventStatus(): EventStatus {
  const randomIndex = Math.floor(Math.random() * EVENT_STATUSES.length);
  return EVENT_STATUSES[randomIndex];
}

/**
 * Strip HTML tags from string
 */
function stripHtml(html: string): string {
  if (!html) return '';
  return html
    .replace(/<[^>]*>/g, ' ')
    .replace(/&nbsp;/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, ' ')
    .trim();
}

/**
 * Parse Vietnamese address into components
 * Example: "Đường Đinh Thị Thi, Phường Hiệp Bình Phước, Quận Thủ Đức, Thành Phố Hồ Chí Minh"
 */
function parseAddress(address: string): ParsedAddress {
  const parts = address.split(',').map((part) => part.trim());

  let street = '';
  let ward: string | null = null;
  let district: string | null = null;
  let city = '';

  if (parts.length >= 1) street = parts[0];
  if (parts.length >= 2) ward = parts[1].replace(/^(Phường|Xã)\s+/i, '');
  if (parts.length >= 3) district = parts[2].replace(/^(Quận|Huyện)\s+/i, '');
  if (parts.length >= 4) city = parts[3].replace(/^(Thành Phố|Tỉnh)\s+/i, '');

  return {
    street: street || 'Unknown',
    ward,
    district,
    city: city || 'Ho Chi Minh City',
    country: 'Vietnam',
  };
}

/**
 * Fetch events from TicketBox search API with pagination
 */
async function fetchTicketBoxEvents(
  page: number,
  limit: number = EVENTS_PER_PAGE,
): Promise<TicketBoxEvent[]> {
  try {
    console.log(`Fetching page ${page} from TicketBox search API...`);

    const response = await axios.get<TicketBoxResponse>(TICKETBOX_SEARCH_API, {
      params: {
        limit,
        page,
        categories: 'theatersandart',
      },
      headers: {
        accept: '*/*',
        'accept-language': 'en,vi-VN;q=0.9,vi;q=0.8,fr-FR;q=0.7,fr;q=0.6,en-US;q=0.5',
        'content-type': 'application/json',
        origin: 'https://ticketbox.vn',
        referer: 'https://ticketbox.vn/',
        'user-agent':
          'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36',
      },
    });

    if (response.data.code === 200) {
      console.log(`✓ Fetched ${response.data.data.results.length} events from page ${page}`);
      return response.data.data.results;
    } else {
      console.error(`Failed to fetch events: ${response.data.message}`);
      return [];
    }
  } catch (error) {
    console.error(`Error fetching page ${page}:`, error.message);
    return [];
  }
}

/**
 * Fetch detailed event data from TicketBox detail API
 */
async function fetchEventDetail(eventId: number): Promise<TicketBoxEventDetail | null> {
  try {
    const response = await axios.get<TicketBoxDetailResponse>(
      `${TICKETBOX_DETAIL_API}/${eventId}`,
      {
        headers: {
          accept: '*/*',
          'accept-language': 'en,vi-VN;q=0.9,vi;q=0.8,fr-FR;q=0.7,fr;q=0.6,en-US;q=0.5',
          'content-type': 'application/json',
          origin: 'https://ticketbox.vn',
          referer: 'https://ticketbox.vn/',
          'user-agent':
            'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36',
        },
      },
    );

    if (response.data.status === 1) {
      return response.data.data.result;
    } else {
      console.error(`Failed to fetch event detail for ID ${eventId}: ${response.data.message}`);
      return null;
    }
  } catch (error) {
    console.error(`Error fetching event detail for ID ${eventId}:`, error.message);
    return null;
  }
}

/**
 * Create or find organizer by name
 */
async function createOrFindOrganizer(name: string, description: string, logoUrl: string) {
  const existingOrganizer = await prisma.organizer.findFirst({
    where: { name },
  });

  if (existingOrganizer) {
    return existingOrganizer;
  }

  const organizer = await prisma.organizer.create({
    data: {
      name,
      description,
      logoUrl,
    },
  });

  console.log(`✓ Created organizer: ${name}`);
  return organizer;
}

/**
 * Verify music category exists
 */
async function verifyMusicCategory() {
  const category = await prisma.category.findUnique({
    where: { id: MUSIC_CATEGORY_ID },
  });

  if (!category) {
    throw new Error(
      `Music category with ID ${MUSIC_CATEGORY_ID} not found. Please ensure the category exists.`,
    );
  }

  console.log(`✓ Music category verified: ${category.name}`);
  return category;
}

/**
 * Create event with all details including location
 */
async function createEvent(
  basicEvent: TicketBoxEvent,
  detailEvent: TicketBoxEventDetail | null,
  organizerId: string,
) {
  const startDate = detailEvent?.startTime
    ? new Date(detailEvent.startTime)
    : new Date(basicEvent.day);
  const endDate = detailEvent?.endTime ? new Date(detailEvent.endTime) : new Date(basicEvent.day);

  const ticketSaleStartDate = new Date(startDate);
  ticketSaleStartDate.setDate(startDate.getDate() - 30); // 30 days before event
  const ticketSaleEndDate = new Date(startDate); // Sale ends at event start

  // Prepare description
  const description = detailEvent?.description
    ? stripHtml(detailEvent.description)
    : basicEvent.name;

  // Prepare location data if available
  const locationData =
    detailEvent?.venue && detailEvent?.address
      ? {
          create: {
            venue: detailEvent.venue,
            ...parseAddress(detailEvent.address),
          },
        }
      : undefined;

  // Create event with nested config, location, and category
  const event = await prisma.event.create({
    data: {
      name: basicEvent.name,
      description: description.substring(0, 500), // Limit description length
      startDate,
      endDate,
      thumbnailUrl: basicEvent.imageUrl,
      status: getRandomEventStatus(),
      organizer: {
        connect: { id: organizerId },
      },
      location: locationData,
      config: {
        create: {
          ticketSaleStartDate,
          ticketSaleEndDate,
          isFree: basicEvent.price === 0,
          maxAttendees: 1000,
          isPublic: true,
          requiresApproval: false,
          allowWaitRoom: true,
          isNewTrending: false,
        },
      },
      categories: {
        create: {
          categoryId: MUSIC_CATEGORY_ID,
        },
      },
    },
    include: {
      config: true,
      location: true,
      categories: true,
      organizer: true,
    },
  });

  return event;
}

/**
 * Main seed function
 */
async function main() {
  console.log('\n🌱 Starting TicketBox event seeding...\n');

  try {
    // Verify music category exists
    await verifyMusicCategory();

    // Fetch all events from TicketBox search API
    const allEvents: TicketBoxEvent[] = [];
    for (let page = 1; page <= TOTAL_PAGES; page++) {
      const events = await fetchTicketBoxEvents(page);
      allEvents.push(...events);
    }

    console.log(`\n✓ Total events fetched: ${allEvents.length}\n`);

    if (allEvents.length === 0) {
      console.log('⚠️  No events to seed. Exiting...');
      return;
    }

    // Create events with detailed information
    console.log('📝 Fetching event details and creating events...\n');
    let successCount = 0;
    let errorCount = 0;
    const organizersCache = new Map<string, string>(); // orgName -> organizerId

    for (const basicEvent of allEvents) {
      try {
        // Fetch detailed event data
        console.log(`  Fetching details for: ${basicEvent.name}...`);
        const detailEvent = await fetchEventDetail(basicEvent.id);

        // Create or find organizer
        const orgName = detailEvent?.orgName || 'Unknown Organizer';
        const orgDescription = detailEvent?.orgDescription || 'Organizer from TicketBox';
        const orgLogoUrl = detailEvent?.orgLogoURL || basicEvent.orgLogoUrl;

        let organizerId = organizersCache.get(orgName);
        if (!organizerId) {
          const organizer = await createOrFindOrganizer(orgName, orgDescription, orgLogoUrl);
          organizerId = organizer.id;
          organizersCache.set(orgName, organizerId);
        }

        // Create event with all details
        const event = await createEvent(basicEvent, detailEvent, organizerId);
        console.log(
          `  ✓ Created: ${event.name} (Status: ${event.status}, Location: ${event.location?.venue || 'N/A'})`,
        );
        successCount++;

        // Small delay to avoid overwhelming the API
        await new Promise((resolve) => setTimeout(resolve, 300));
      } catch (error) {
        console.error(`  ✗ Failed to create event "${basicEvent.name}": ${error.message}`);
        errorCount++;
      }
    }

    // Summary
    console.log('\n' + '='.repeat(60));
    console.log('📊 SEED SUMMARY');
    console.log('='.repeat(60));
    console.log(`Total events fetched:    ${allEvents.length}`);
    console.log(`Successfully created:    ${successCount}`);
    console.log(`Failed:                  ${errorCount}`);
    console.log(`Unique organizers:       ${organizersCache.size}`);
    console.log(`Category:                Music (${MUSIC_CATEGORY_ID})`);
    console.log('='.repeat(60) + '\n');

    console.log('✅ Seeding completed successfully!\n');
  } catch (error) {
    console.error('\n❌ Seeding failed:', error.message);
    throw error;
  }
}

// Execute seed
main()
  .catch((error) => {
    console.error(error);
    process.exit(1);
  })
  .finally(async () => {
    await prisma.$disconnect();
  });
