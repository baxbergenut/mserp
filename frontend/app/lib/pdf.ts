export async function renderPDFPages(file: File): Promise<Blob[]> {
  const pdfjs = await import("pdfjs-dist");
  pdfjs.GlobalWorkerOptions.workerSrc = new URL(
    "pdfjs-dist/build/pdf.worker.min.mjs",
    import.meta.url,
  ).toString();

  const loadingTask = pdfjs.getDocument({
    data: new Uint8Array(await file.arrayBuffer()),
  });
  const document = await loadingTask.promise;

  try {
    const pageCount = Math.min(document.numPages, 3);
    const images: Blob[] = [];
    for (let pageNumber = 1; pageNumber <= pageCount; pageNumber += 1) {
      const page = await document.getPage(pageNumber);
      const initialViewport = page.getViewport({ scale: 1 });
      const scale = Math.min(
        2.5,
        2200 / Math.max(initialViewport.width, initialViewport.height),
      );
      const viewport = page.getViewport({ scale });
      const canvas = documentCanvas(viewport.width, viewport.height);

      await page.render({ canvas, viewport }).promise;
      images.push(await canvasToJPEG(canvas));
      page.cleanup();
    }
    return images;
  } finally {
    await loadingTask.destroy();
  }
}

function documentCanvas(width: number, height: number): HTMLCanvasElement {
  const canvas = window.document.createElement("canvas");
  canvas.width = Math.ceil(width);
  canvas.height = Math.ceil(height);
  return canvas;
}

function canvasToJPEG(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) resolve(blob);
        else reject(new Error("The PDF page could not be rendered."));
      },
      "image/jpeg",
      0.9,
    );
  });
}
