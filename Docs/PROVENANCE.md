# Clean-room provenance

This project implements behavior required for interoperability with a lawfully obtained Rongta F11 printer.

The implementation was developed from observed official-driver output, controlled differential fixtures, public USB behavior, and independently written encode/decode tests. The clean-room source in this repository was authored for this project. It does not include:

- Rongta binaries or libraries;
- PPD, INF, XPD, or installer files;
- copied vendor source;
- disassembly listings;
- encryption keys or unrelated dormant vendor capabilities;
- proprietary artwork or trademarks beyond nominative compatibility references; or
- captured user documents.

The repository documents only the interoperability subset needed to construct ordinary F11 page streams.

A physically successful official stream was used during local research to validate framing and decoding. It is not distributed here. Public tests use synthetic data and published golden bytes limited to protocol facts.

The independent clean-room encoder was physically accepted by the tested printer, demonstrating that vendor-byte identity and vendor-generated Huffman trees are not required.

Contributors must not submit proprietary binaries, decompiled source, copied code, confidential SDK material, or files whose redistribution rights are unclear. Describe observable behavior and add synthetic tests instead.

This document is a technical provenance statement, not legal advice. Laws governing interoperability and reverse engineering vary by jurisdiction.
