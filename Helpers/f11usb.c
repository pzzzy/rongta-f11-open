#include <libusb.h>

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#define F11_VID 0x0fe6
#define F11_PID 0x811e
#define F11_INTERFACE 0
#define F11_ENDPOINT_OUT 0x01
#define CHUNK_SIZE 2048
#define TRANSFER_TIMEOUT_MS 5000
#define MAX_STREAM_SIZE (64L * 1024L * 1024L)

static int load_file(const char *path, uint8_t **data_out, long *size_out) {
    FILE *file = fopen(path, "rb");
    if (file == NULL) {
        perror(path);
        return 1;
    }

    if (fseek(file, 0, SEEK_END) != 0) {
        perror("fseek");
        fclose(file);
        return 1;
    }
    long size = ftell(file);
    if (size <= 0 || size > MAX_STREAM_SIZE) {
        fprintf(stderr, "invalid stream size: %ld bytes\n", size);
        fclose(file);
        return 1;
    }
    rewind(file);

    uint8_t *data = malloc((size_t)size);
    if (data == NULL) {
        fprintf(stderr, "allocation failed for %ld bytes\n", size);
        fclose(file);
        return 1;
    }
    if (fread(data, 1, (size_t)size, file) != (size_t)size) {
        fprintf(stderr, "could not read complete stream\n");
        free(data);
        fclose(file);
        return 1;
    }
    if (fclose(file) != 0) {
        perror("fclose");
        free(data);
        return 1;
    }

    *data_out = data;
    *size_out = size;
    return 0;
}

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: f11usb stream.f11\n");
        return 2;
    }

    uint8_t *data = NULL;
    long size = 0;
    if (load_file(argv[1], &data, &size) != 0) {
        return 1;
    }

    libusb_context *context = NULL;
    libusb_device_handle *device = NULL;
    int result = libusb_init(&context);
    if (result < 0) {
        fprintf(stderr, "libusb_init: %s\n", libusb_error_name(result));
        free(data);
        return 1;
    }

    device = libusb_open_device_with_vid_pid(context, F11_VID, F11_PID);
    if (device == NULL) {
        fprintf(stderr, "F11 not found (VID %04x, PID %04x)\n", F11_VID, F11_PID);
        libusb_exit(context);
        free(data);
        return 3;
    }

    (void)libusb_set_auto_detach_kernel_driver(device, 1);
    result = libusb_claim_interface(device, F11_INTERFACE);
    if (result < 0) {
        fprintf(stderr, "claim interface: %s\n", libusb_error_name(result));
        libusb_close(device);
        libusb_exit(context);
        free(data);
        return 1;
    }

    long sent = 0;
    int chunks = 0;
    for (long offset = 0; offset < size; offset += CHUNK_SIZE) {
        int requested = (int)((size - offset) > CHUNK_SIZE ? CHUNK_SIZE : (size - offset));
        int actual = 0;
        result = libusb_bulk_transfer(
            device,
            F11_ENDPOINT_OUT,
            data + offset,
            requested,
            &actual,
            TRANSFER_TIMEOUT_MS
        );
        if (result < 0 || actual != requested) {
            fprintf(
                stderr,
                "USB write at %ld: %s (%d/%d bytes)\n",
                offset,
                libusb_error_name(result),
                actual,
                requested
            );
            libusb_release_interface(device, F11_INTERFACE);
            libusb_close(device);
            libusb_exit(context);
            free(data);
            return 1;
        }
        sent += actual;
        chunks += 1;
        usleep(4000);
    }

    printf("{\"bytes_sent\":%ld,\"chunks\":%d}\n", sent, chunks);
    libusb_release_interface(device, F11_INTERFACE);
    libusb_close(device);
    libusb_exit(context);
    free(data);
    return 0;
}
