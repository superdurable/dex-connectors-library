// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.type.LogicalType;

import java.io.IOException;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.Pattern;

final class PersonsQueries {
    private static final Pattern PERSON_ID = Pattern.compile("[A-Za-z0-9_-]{1,128}");
    private static final ObjectMapper WIRE =
            PersonioClient.JSON
                    .copy()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);

    static {
        MutableCoercionConfig strings = WIRE.coercionConfigFor(LogicalType.Textual);
        for (CoercionInputShape shape :
                Arrays.asList(
                        CoercionInputShape.Integer,
                        CoercionInputShape.Float,
                        CoercionInputShape.Boolean))
            strings.setCoercion(shape, CoercionAction.Fail);
    }

    private PersonsQueries() {}

    static final class Get extends PersonioClient.PersonQuery<GetPersonInput, PersonProfile> {
        Get(PersonioClient client) {
            super(client, "getPerson", new PersonProfile("", "", ""));
        }

        String successBranch() {
            return "found";
        }

        String suffix(GetPersonInput input) {
            if (input == null || !PERSON_ID.matcher(input.personId).matches())
                throw new IllegalArgumentException("Invalid person ID");
            return "/" + input.personId;
        }

        PersonProfile decode(byte[] body, GetPersonInput input, String baseUrl) throws IOException {
            PersonProfile profile = profile(WIRE.readValue(body, WirePerson.class));
            if (!input.personId.equals(profile.id)) throw new IOException("Mismatched person ID");
            return profile;
        }
    }

    static final class ListPage extends PersonioClient.PersonQuery<ListPersonsInput, PersonPage> {
        ListPage(PersonioClient client) {
            super(client, "listPersons", new PersonPage(Collections.emptyList(), ""));
        }

        String successBranch() {
            return "listed";
        }

        String suffix(ListPersonsInput input) throws IOException {
            if (input == null
                    || input.limit < 0
                    || input.limit > 50
                    || !(input.status.isEmpty()
                            || input.status.equals("ACTIVE")
                            || input.status.equals("INACTIVE"))
                    || input.cursor.getBytes(StandardCharsets.UTF_8).length > 1024)
                throw new IllegalArgumentException("Invalid list filters");
            StringJoiner query = new StringJoiner("&", "?", "").setEmptyValue("");
            append(query, "cursor", input.cursor);
            append(query, "first_name", input.firstName);
            append(query, "last_name", input.lastName);
            if (input.limit > 0) append(query, "limit", Long.toString(input.limit));
            append(query, "status", input.status);
            return query.toString();
        }

        PersonPage decode(byte[] body, ListPersonsInput input, String baseUrl) throws IOException {
            WirePage page = WIRE.readValue(body, WirePage.class);
            if (page == null || page.persons == null) throw new IOException("Missing Persons page");
            List<PersonProfile> persons = new ArrayList<>(page.persons.size());
            for (WirePerson person : page.persons) persons.add(profile(person));
            String href =
                    page.meta == null || page.meta.links == null || page.meta.links.next == null
                            ? ""
                            : page.meta.links.next.href;
            return new PersonPage(
                    persons, href == null || href.isEmpty() ? "" : nextCursor(href, baseUrl));
        }
    }

    private static void append(StringJoiner query, String key, String value) throws IOException {
        if (!value.isEmpty()) query.add(key + "=" + URLEncoder.encode(value, "UTF-8"));
    }

    private static String nextCursor(String href, String baseUrl) throws IOException {
        if (href.chars().anyMatch(c -> c < 32 || c == 127))
            throw new IOException("Invalid next link");
        String link = href.split("#", 2)[0];
        int queryStart = link.indexOf('?');
        if (queryStart < 0) throw new IOException("Missing next cursor");
        URI location = URI.create(link.substring(0, queryStart));
        URI base = URI.create(baseUrl);
        String authority = location.getRawAuthority();
        if (authority != null) authority = authority.substring(authority.lastIndexOf('@') + 1);
        if (!"/v2/persons".equals(location.getPath())
                || (location.isAbsolute()
                        && (!base.getScheme().equalsIgnoreCase(location.getScheme())
                                || !base.getRawAuthority().equals(authority))))
            throw new IOException("Invalid next link");
        for (String pair : link.substring(queryStart + 1).split("&")) {
            if (pair.indexOf(';') >= 0) continue;
            String[] fields = pair.split("=", 2);
            String name, value;
            try {
                name = URLDecoder.decode(fields[0], "UTF-8");
                value = fields.length == 1 ? "" : URLDecoder.decode(fields[1], "UTF-8");
            } catch (IllegalArgumentException e) {
                continue;
            }
            if (name.equals("cursor")) {
                if (value.isEmpty()) break;
                return value;
            }
        }
        throw new IOException("Missing next cursor");
    }

    private static PersonProfile profile(WirePerson person) throws IOException {
        if (person == null || person.id == null || !PERSON_ID.matcher(person.id).matches())
            throw new IOException("Invalid Personio profile");
        return new PersonProfile(person.id, person.firstName, person.lastName);
    }

    private static final class WirePerson {
        public String id;

        @JsonProperty("first_name")
        public String firstName;

        @JsonProperty("last_name")
        public String lastName;
    }

    private static final class WirePage {
        @JsonProperty("_data")
        public List<WirePerson> persons;

        @JsonProperty("_meta")
        public Meta meta;
    }

    private static final class Meta {
        public Links links;
    }

    private static final class Links {
        public Next next;
    }

    private static final class Next {
        public String href;
    }
}
